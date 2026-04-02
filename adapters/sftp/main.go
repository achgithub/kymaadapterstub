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
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type AdapterConfig struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Type                 string       `json:"type"`
	BehaviorMode         string       `json:"behavior_mode"`
	Files                []FileConfig `json:"files"`
	AuthMode             string       `json:"auth_mode"`
	SSHHostKey           string       `json:"ssh_host_key"`
	SSHHostKeyFingerprint string      `json:"ssh_host_key_fingerprint"`
	Credentials          *Credentials `json:"credentials"`
}

type FileConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type InMemoryFile struct {
	name    string
	content []byte
	size    int64
	modTime time.Time
}

func main() {
	adapterID := os.Getenv("ADAPTER_ID")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")

	if adapterID == "" {
		log.Fatal("ADAPTER_ID environment variable is required")
	}

	if controlPlaneURL == "" {
		controlPlaneURL = "http://control-plane:8080"
	}

	log.Printf("SFTP Adapter started")
	log.Printf("Adapter ID: %s", adapterID)
	log.Printf("Control Plane URL: %s", controlPlaneURL)

	// Fetch config on startup
	config, err := fetchConfig(adapterID, controlPlaneURL)
	if err != nil {
		log.Fatalf("Failed to fetch initial config: %v", err)
	}

	log.Printf("Config loaded: %d files", len(config.Files))

	// Load or generate SSH host key
	hostKey, err := loadOrGenerateHostKey(config.SSHHostKey)
	if err != nil {
		log.Fatalf("Failed to load host key: %v", err)
	}

	fingerprint := ssh.FingerprintSHA256(hostKey.PublicKey())
	if config.SSHHostKeyFingerprint != "" {
		log.Printf("SSH host key fingerprint: %s", config.SSHHostKeyFingerprint)
	} else {
		log.Printf("SSH host key fingerprint: %s", fingerprint)
	}

	// Create SSH server config
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			return handleAuth(conn.User(), string(pass), config)
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// Reject public key auth
			return nil, fmt.Errorf("public key authentication not supported")
		},
	}

	sshConfig.AddHostKey(hostKey)

	// Listen for SSH connections
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

		go handleConnection(conn, sshConfig, config)
	}
}

func handleConnection(conn net.Conn, sshConfig *ssh.ServerConfig, config *AdapterConfig) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		log.Printf("SSH handshake error: %v", err)
		return
	}
	defer sshConn.Close()

	log.Printf("SSH login from %s as %s", sshConn.RemoteAddr(), sshConn.User())

	go ssh.DiscardRequests(reqs)

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

		go handleChannel(channel, requests, config)
	}
}

func handleChannel(channel ssh.Channel, requests <-chan *ssh.Request, config *AdapterConfig) {
	defer channel.Close()

	for req := range requests {
		if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
			req.Reply(true, nil)

			// Create SFTP server
			files := convertFilesToMemory(config.Files)
			server := sftp.NewRequestServer(channel, sftp.Handlers{
				FileGet:  &SFTPHandler{files: files},
				FilePut:  &SFTPHandler{files: files},
				FileList: &SFTPHandler{files: files},
				FileCmd:  &SFTPHandler{files: files},
			})

			if err := server.Serve(); err != nil && err != io.EOF {
				log.Printf("SFTP server error: %v", err)
			}
			return
		}

		req.Reply(false, nil)
	}
}

func handleAuth(user, pass string, config *AdapterConfig) (*ssh.Permissions, error) {
	// Check behavior mode
	if config.AuthMode == "failure" {
		return nil, fmt.Errorf("authentication failed")
	}

	// Check credentials if provided
	if config.Credentials != nil {
		if user == config.Credentials.Username && pass == config.Credentials.Password {
			return &ssh.Permissions{}, nil
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Accept any credentials in success mode
	return &ssh.Permissions{}, nil
}

type SFTPHandler struct {
	files map[string]*InMemoryFile
}

func (h *SFTPHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	file, exists := h.files[r.Filepath]
	if !exists {
		return nil, sftp.ErrSSHFxNoSuchFile
	}

	return &fileReader{data: file.content}, nil
}

func (h *SFTPHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	path := r.Filepath
	if path == "/" || path == "" {
		return &fileLister{files: h.files}, nil
	}

	// Return the single file if requested
	if f, exists := h.files[path]; exists {
		return &fileLister{files: map[string]*InMemoryFile{path: f}}, nil
	}

	return nil, sftp.ErrSSHFxNoSuchFile
}

func (h *SFTPHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	// For MVP, prevent writes
	return nil, fmt.Errorf("write not supported")
}

func (h *SFTPHandler) Filecmd(r *sftp.Request) error {
	// Handle mkdir, remove, rename, etc.
	switch r.Method {
	case "mkdir":
		return fmt.Errorf("mkdir not supported")
	case "remove":
		return fmt.Errorf("remove not supported")
	case "rename":
		return fmt.Errorf("rename not supported")
	default:
		return fmt.Errorf("unsupported command: %s", r.Method)
	}
}

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

type fileLister struct {
	files map[string]*InMemoryFile
}

func (f *fileLister) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	fileList := make([]*InMemoryFile, 0, len(f.files))
	for _, file := range f.files {
		fileList = append(fileList, file)
	}

	idx := int(offset)
	if idx >= len(fileList) {
		return 0, io.EOF
	}

	count := 0
	for idx < len(fileList) && count < len(ls) {
		file := fileList[idx]
		ls[count] = &fileInfo{
			name:    file.name,
			size:    file.size,
			modTime: file.modTime,
		}
		idx++
		count++
	}

	if idx >= len(fileList) {
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

func convertFilesToMemory(files []FileConfig) map[string]*InMemoryFile {
	result := make(map[string]*InMemoryFile)
	now := time.Now()

	for _, f := range files {
		data := []byte(f.Content)
		result[f.Name] = &InMemoryFile{
			name:    f.Name,
			content: data,
			size:    int64(len(data)),
			modTime: now,
		}
	}

	return result
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

	// No key in config — generate a temporary one (fingerprint changes on restart)
	log.Printf("Warning: no SSH host key in config — generating a temporary key. Fingerprint will change on each restart.")
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}
	return ssh.NewSignerFromKey(privateKey)
}
