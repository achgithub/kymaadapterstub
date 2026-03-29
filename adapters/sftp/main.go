package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
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
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	BehaviorMode string           `json:"behavior_mode"`
	Files        []FileConfig     `json:"files"`
	AuthMode     string           `json:"auth_mode"`
	Credentials  *Credentials     `json:"credentials"`
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

	// Generate SSH key pair
	hostKey, err := generateHostKey()
	if err != nil {
		log.Fatalf("Failed to generate host key: %v", err)
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

	log.Printf("SSH login attempt from %s as %s", sshConn.RemoteAddr(), sshConn.User())

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

			// Start SFTP server
			server := sftp.NewRequestServer(channel, &SFTPHandler{
				files: convertFilesToMemory(config.Files),
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
	file, exists := h.files[r.Filename]
	if !exists {
		return nil, sftp.ErrSSHFxNoSuchFile
	}

	return &fileReader{data: file.content}, nil
}

func (h *SFTPHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	return &fileLister{files: h.files}, nil
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

func (h *SFTPHandler) Stat(r *sftp.Request) (sftp.ListerAt, error) {
	file, exists := h.files[r.Filename]
	if !exists {
		return nil, sftp.ErrSSHFxNoSuchFile
	}

	return &fileLister{files: map[string]*InMemoryFile{r.Filename: file}}, nil
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

func (f *fileLister) ListAt(ls []sftp.FileInfo, offset int64) (int, error) {
	i := int(offset)
	fileList := make([]*InMemoryFile, 0, len(f.files))
	for _, file := range f.files {
		fileList = append(fileList, file)
	}

	if i >= len(fileList) {
		return 0, io.EOF
	}

	for i < len(fileList) && i < len(ls)+int(offset) {
		file := fileList[i]
		ls[i-int(offset)] = &fileInfo{
			name:    file.name,
			size:    file.size,
			modTime: file.modTime,
		}
		i++
	}

	if i >= len(fileList) {
		return i - int(offset), io.EOF
	}

	return i - int(offset), nil
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

func generateHostKey() (ssh.Signer, error) {
	// Generate a random RSA private key for this instance
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return signer, nil
}
