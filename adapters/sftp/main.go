package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type AdapterConfig struct {
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	Type                  string       `json:"type"`
	BehaviorMode          string       `json:"behavior_mode"`
	Files                 []FileConfig `json:"files"`
	AuthMode              string       `json:"auth_mode"`
	SSHHostKey            string       `json:"ssh_host_key"`
	SSHHostKeyFingerprint string       `json:"ssh_host_key_fingerprint"`
	SSHPublicKey          string       `json:"ssh_public_key"` // authorized_keys format; if empty, accept any key
	Credentials           *Credentials `json:"credentials"`
}

type FileConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// InMemoryFile is a single file held in the in-memory store.
type InMemoryFile struct {
	name    string
	content []byte
	size    int64
	modTime time.Time
}

// fileStore is a thread-safe in-memory file store that supports read, write,
// delete, and rename — all operations CPI post-processing may require.
type fileStore struct {
	mu    sync.Mutex
	files map[string]*InMemoryFile
}

func newFileStore(configs []FileConfig) *fileStore {
	fs := &fileStore{files: make(map[string]*InMemoryFile)}
	now := time.Now()
	for _, f := range configs {
		data := []byte(f.Content)
		fs.files[f.Name] = &InMemoryFile{
			name:    f.Name,
			content: data,
			size:    int64(len(data)),
			modTime: now,
		}
	}
	return fs
}

func (fs *fileStore) get(path string) (*InMemoryFile, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.files[path]
	return f, ok
}

func (fs *fileStore) list() []*InMemoryFile {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]*InMemoryFile, 0, len(fs.files))
	for _, f := range fs.files {
		out = append(out, f)
	}
	return out
}

func (fs *fileStore) delete(path string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.files[path]; !ok {
		return sftp.ErrSSHFxNoSuchFile
	}
	delete(fs.files, path)
	log.Printf("SFTP delete: %s", path)
	return nil
}

func (fs *fileStore) rename(oldPath, newPath string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.files[oldPath]
	if !ok {
		return sftp.ErrSSHFxNoSuchFile
	}
	f.name = newPath
	fs.files[newPath] = f
	delete(fs.files, oldPath)
	log.Printf("SFTP rename: %s → %s", oldPath, newPath)
	return nil
}

func (fs *fileStore) write(path string, data []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[path] = &InMemoryFile{
		name:    path,
		content: data,
		size:    int64(len(data)),
		modTime: time.Now(),
	}
	log.Printf("SFTP write: %s (%d bytes)", path, len(data))
}

// fileWriter buffers a write operation and commits to the store on Close.
type fileWriter struct {
	store *fileStore
	path  string
	mu    sync.Mutex
	data  []byte
}

func (fw *fileWriter) WriteAt(p []byte, off int64) (int, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	end := off + int64(len(p))
	if end > int64(len(fw.data)) {
		grown := make([]byte, end)
		copy(grown, fw.data)
		fw.data = grown
	}
	copy(fw.data[off:], p)
	return len(p), nil
}

func (fw *fileWriter) Close() error {
	fw.store.write(fw.path, fw.data)
	return nil
}

// SFTPHandler implements all four sftp.Handlers interfaces.
type SFTPHandler struct {
	store *fileStore
}

func (h *SFTPHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	f, ok := h.store.get(r.Filepath)
	if !ok {
		return nil, sftp.ErrSSHFxNoSuchFile
	}
	return &fileReader{data: f.content}, nil
}

func (h *SFTPHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	path := r.Filepath
	if path == "/" || path == "" || path == "." {
		return &fileLister{files: h.store.list()}, nil
	}
	// Stat a specific file
	if f, ok := h.store.get(path); ok {
		return &fileLister{files: []*InMemoryFile{f}}, nil
	}
	return nil, sftp.ErrSSHFxNoSuchFile
}

func (h *SFTPHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return &fileWriter{store: h.store, path: r.Filepath}, nil
}

func (h *SFTPHandler) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "remove", "Remove":
		return h.store.delete(r.Filepath)
	case "rename", "Rename":
		return h.store.rename(r.Filepath, r.Target)
	case "mkdir", "Mkdir":
		// Accept silently — CPI may create directories before writing
		log.Printf("SFTP mkdir (no-op): %s", r.Filepath)
		return nil
	case "rmdir", "Rmdir":
		log.Printf("SFTP rmdir (no-op): %s", r.Filepath)
		return nil
	case "setstat", "Setstat":
		// Ignore attribute updates
		return nil
	default:
		log.Printf("SFTP unsupported cmd: %s on %s", r.Method, r.Filepath)
		return fmt.Errorf("unsupported command: %s", r.Method)
	}
}

// fileReader implements io.ReaderAt over a byte slice.
type fileReader struct {
	data []byte
}

func (f *fileReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if off+int64(n) >= int64(len(f.data)) {
		return n, io.EOF
	}
	return n, nil
}

// fileLister implements sftp.ListerAt over a slice of InMemoryFile.
type fileLister struct {
	files []*InMemoryFile
}

func (fl *fileLister) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	idx := int(offset)
	if idx >= len(fl.files) {
		return 0, io.EOF
	}
	count := 0
	for idx < len(fl.files) && count < len(ls) {
		f := fl.files[idx]
		ls[count] = &fileInfo{name: f.name, size: f.size, modTime: f.modTime}
		idx++
		count++
	}
	if idx >= len(fl.files) {
		return count, io.EOF
	}
	return count, nil
}

type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (f *fileInfo) Name() string       { return f.name }
func (f *fileInfo) Size() int64        { return f.size }
func (f *fileInfo) Mode() os.FileMode  { return 0644 }
func (f *fileInfo) ModTime() time.Time { return f.modTime }
func (f *fileInfo) IsDir() bool        { return false }
func (f *fileInfo) Sys() interface{}   { return nil }

func main() {
	adapterID := os.Getenv("ADAPTER_ID")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")

	if adapterID == "" {
		log.Fatal("ADAPTER_ID environment variable is required")
	}
	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	log.Printf("SFTP Adapter started (ID: %s)", adapterID)

	// Fetch initial config to get the host key.
	// Non-fatal: if the control plane isn't ready yet (e.g. Docker startup race),
	// fall back to a temporary generated key. Per-connection config fetches will
	// pick up the real config once a scenario is launched.
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Warning: failed to fetch initial config: %v — using temporary SSH host key", err)
		config = &AdapterConfig{}
	}

	hostKey, err := loadOrGenerateHostKey(config.SSHHostKey)
	if err != nil {
		log.Fatalf("Failed to load host key: %v", err)
	}
	log.Printf("SSH host key fingerprint: %s", ssh.FingerprintSHA256(hostKey.PublicKey()))

	// SSH server config — auth callbacks fetch fresh config on each attempt.
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			cfg, err := fetchConfig(adapterID, controlPlaneURL)
			if err != nil {
				return nil, fmt.Errorf("config unavailable")
			}
			return handlePasswordAuth(conn.User(), string(pass), cfg)
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			cfg, err := fetchConfig(adapterID, controlPlaneURL)
			if err != nil {
				return nil, fmt.Errorf("config unavailable")
			}
			return handlePublicKeyAuth(conn.User(), key, cfg)
		},
	}
	sshConfig.AddHostKey(hostKey)

	listener, err := net.Listen("tcp", ":22")
	if err != nil {
		log.Fatalf("Failed to listen on port 22: %v", err)
	}
	defer listener.Close()
	log.Printf("SFTP Server listening on :22")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn, sshConfig, adapterID, controlPlaneURL)
	}
}

func reportActivity(adapterID, controlPlaneURL string) {
	go func() {
		c := &http.Client{Timeout: 2 * time.Second}
		c.Post(fmt.Sprintf("%s/api/adapter-activity/%s", controlPlaneURL, adapterID), "application/json", nil)
	}()
}

func handleConnection(conn net.Conn, sshConfig *ssh.ServerConfig, adapterID, controlPlaneURL string) {
	reportActivity(adapterID, controlPlaneURL)
	defer conn.Close()

	// Fetch fresh config for this connection — picks up file/credential changes without restart.
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Printf("Failed to fetch config for connection: %v", err)
		return
	}

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		// EOF = probe (k8s liveness/readiness) or client disconnect before handshake — not a real error
		if err.Error() != "EOF" {
			log.Printf("SSH handshake error: %v", err)
		}
		return
	}
	defer sshConn.Close()
	log.Printf("SSH login from %s as %s", sshConn.RemoteAddr(), sshConn.User())

	go ssh.DiscardRequests(reqs)

	store := newFileStore(config.Files)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("Could not accept channel: %v", err)
			continue
		}
		go handleChannel(channel, requests, store)
	}
}

func handleChannel(channel ssh.Channel, requests <-chan *ssh.Request, store *fileStore) {
	defer channel.Close()
	for req := range requests {
		if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
			req.Reply(true, nil)
			handler := &SFTPHandler{store: store}
			server := sftp.NewRequestServer(channel, sftp.Handlers{
				FileGet:  handler,
				FilePut:  handler,
				FileList: handler,
				FileCmd:  handler,
			})
			if err := server.Serve(); err != nil && err != io.EOF {
				log.Printf("SFTP server error: %v", err)
			}
			return
		}
		req.Reply(false, nil)
	}
}

func handlePasswordAuth(user, pass string, config *AdapterConfig) (*ssh.Permissions, error) {
	if config.BehaviorMode == "failure" {
		return nil, fmt.Errorf("authentication failed")
	}
	if config.Credentials != nil {
		if user == config.Credentials.Username && pass == config.Credentials.Password {
			return &ssh.Permissions{}, nil
		}
		return nil, fmt.Errorf("invalid credentials")
	}
	// No credentials configured — accept anything
	return &ssh.Permissions{}, nil
}

func handlePublicKeyAuth(user string, key ssh.PublicKey, config *AdapterConfig) (*ssh.Permissions, error) {
	if config.BehaviorMode == "failure" {
		return nil, fmt.Errorf("authentication failed")
	}
	if config.SSHPublicKey != "" {
		authorizedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(config.SSHPublicKey))
		if err != nil {
			return nil, fmt.Errorf("invalid configured public key")
		}
		if ssh.FingerprintSHA256(authorizedKey) != ssh.FingerprintSHA256(key) {
			return nil, fmt.Errorf("public key not authorized")
		}
		return &ssh.Permissions{}, nil
	}
	// No public key configured — accept any key in success mode
	return &ssh.Permissions{}, nil
}

func fetchConfig(adapterID, controlPlaneURL string) (*AdapterConfig, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/adapter-config/%s", controlPlaneURL, adapterID))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("config endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	var config AdapterConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	return &config, nil
}

// loadOrGenerateHostKey uses the PEM key from config if available,
// otherwise generates a temporary one (fingerprint will change on restart).
func loadOrGenerateHostKey(keyPEM string) (ssh.Signer, error) {
	if keyPEM != "" {
		block, _ := pem.Decode([]byte(keyPEM))
		if block == nil {
			return nil, fmt.Errorf("failed to decode PEM block from config")
		}
		privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		return ssh.NewSignerFromKey(privateKey)
	}
	log.Printf("Warning: no SSH host key in config — generating a temporary key. Fingerprint will change on each restart.")
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}
	return ssh.NewSignerFromKey(privateKey)
}
