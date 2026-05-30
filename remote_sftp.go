package harvey

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// maxSFTPPacketBytes caps the byte length of any single SFTP packet to prevent
// a malicious server from triggering a multi-gigabyte allocation.
const maxSFTPPacketBytes = uint32(4 * 1024 * 1024) // 4 MB

// maxSFTPStringBytes caps individual length-prefixed strings inside packets.
const maxSFTPStringBytes = uint32(256 * 1024) // 256 KB

// SFTP v3 packet type constants (draft-ietf-secsh-filexfer-02 §3).
const (
	sftpFxpInit    = 1
	sftpFxpVersion = 2
	sftpFxpOpen    = 3
	sftpFxpClose   = 4
	sftpFxpRead    = 5
	sftpFxpStat    = 17
	sftpFxpOpendir = 11
	sftpFxpReaddir = 12
	sftpFxpStatus  = 101
	sftpFxpHandle  = 102
	sftpFxpData    = 103
	sftpFxpName    = 104
	sftpFxpAttrs   = 105
)

// SFTP status codes.
const (
	sftpStatusOK  = uint32(0)
	sftpStatusEOF = uint32(1)
)

// sftpAttrSize is the ATTRS flag indicating a size field is present.
const sftpAttrSize = uint32(0x00000001)

// sftpOpenRead is the pflags value for read-only open.
const sftpOpenRead = uint32(0x00000001)

// ─── Binary encoding helpers ──────────────────────────────────────────────────

/** sftpPutU32 encodes v as a big-endian 4-byte slice.
 *
 * Parameters:
 *   v (uint32) — value to encode.
 *
 * Returns:
 *   []byte — 4-byte big-endian encoding.
 *
 * Example:
 *   b := sftpPutU32(3) // [0, 0, 0, 3]
 */
func sftpPutU32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

/** sftpPutU64 encodes v as a big-endian 8-byte slice.
 *
 * Parameters:
 *   v (uint64) — value to encode.
 *
 * Returns:
 *   []byte — 8-byte big-endian encoding.
 *
 * Example:
 *   b := sftpPutU64(1024) // 8-byte slice
 */
func sftpPutU64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

/** sftpPutStr encodes s as a length-prefixed SSH/SFTP string: uint32(len) + bytes.
 *
 * Parameters:
 *   s (string) — the string to encode.
 *
 * Returns:
 *   []byte — 4-byte length header followed by the string bytes.
 *
 * Example:
 *   b := sftpPutStr("sftp") // [0, 0, 0, 4, 's', 'f', 't', 'p']
 */
func sftpPutStr(s string) []byte {
	b := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	copy(b[4:], s)
	return b
}

/** sftpReadU32 reads a big-endian uint32 from r.
 *
 * Parameters:
 *   r (io.Reader) — source to read 4 bytes from.
 *
 * Returns:
 *   uint32 — decoded value.
 *   error  — on read failure.
 *
 * Example:
 *   v, err := sftpReadU32(conn)
 */
func sftpReadU32(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

/** sftpReadU64 reads a big-endian uint64 from r.
 *
 * Parameters:
 *   r (io.Reader) — source to read 8 bytes from.
 *
 * Returns:
 *   uint64 — decoded value.
 *   error  — on read failure.
 *
 * Example:
 *   offset, err := sftpReadU64(conn)
 */
func sftpReadU64(r io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

/** sftpReadStr reads a length-prefixed string from r.
 *
 * Parameters:
 *   r (io.Reader) — source; reads uint32 length then that many bytes.
 *
 * Returns:
 *   string — the decoded string.
 *   error  — on read failure or truncation.
 *
 * Example:
 *   s, err := sftpReadStr(conn)
 */
func sftpReadStr(r io.Reader) (string, error) {
	n, err := sftpReadU32(r)
	if err != nil {
		return "", err
	}
	if n > maxSFTPStringBytes {
		return "", fmt.Errorf("sftp: string too large (%d bytes)", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

// ─── Packet framing ───────────────────────────────────────────────────────────

/** sftpSendPacket writes a framed SFTP packet to w.
 * Wire format: uint32(1+len(payload)) | uint8(pktType) | payload.
 *
 * Parameters:
 *   w       (io.Writer) — destination.
 *   pktType (byte)      — SFTP packet type constant.
 *   payload ([]byte)    — packet body (may be nil).
 *
 * Returns:
 *   error — on write failure.
 *
 * Example:
 *   sftpSendPacket(w, sftpFxpVersion, sftpPutU32(3))
 */
func sftpSendPacket(w io.Writer, pktType byte, payload []byte) error {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
	frame[4] = pktType
	copy(frame[5:], payload)
	_, err := w.Write(frame)
	return err
}

/** sftpReadPacket reads one SFTP packet from r.
 * Returns the packet type byte and the payload (everything after the type byte).
 *
 * Parameters:
 *   r (io.Reader) — source; reads the 4-byte length header, then the packet body.
 *
 * Returns:
 *   byte   — packet type.
 *   []byte — packet payload (excludes the type byte).
 *   error  — on read failure.
 *
 * Example:
 *   pktType, payload, err := sftpReadPacket(channel)
 */
func sftpReadPacket(r io.Reader) (byte, []byte, error) {
	length, err := sftpReadU32(r)
	if err != nil {
		return 0, nil, err
	}
	if length == 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	if length > maxSFTPPacketBytes {
		return 0, nil, fmt.Errorf("sftp: packet too large (%d bytes)", length)
	}
	pkt := make([]byte, length)
	if _, err := io.ReadFull(r, pkt); err != nil {
		return 0, nil, err
	}
	return pkt[0], pkt[1:], nil
}

// ─── packetReader — sequential binary decoder over a byte slice ───────────────

// packetReader provides sequential decoding of an SFTP packet payload.
// Errors are sticky: the first decode error is stored and returned on all
// subsequent reads, so callers do not need per-field error checks.
type packetReader struct {
	data []byte
	err  error
}

func (p *packetReader) readU32() uint32 {
	if p.err != nil {
		return 0
	}
	if len(p.data) < 4 {
		p.err = fmt.Errorf("sftp: short packet (need 4 bytes, have %d)", len(p.data))
		return 0
	}
	v := binary.BigEndian.Uint32(p.data[:4])
	p.data = p.data[4:]
	return v
}

func (p *packetReader) readU64() uint64 {
	if p.err != nil {
		return 0
	}
	if len(p.data) < 8 {
		p.err = fmt.Errorf("sftp: short packet (need 8 bytes, have %d)", len(p.data))
		return 0
	}
	v := binary.BigEndian.Uint64(p.data[:8])
	p.data = p.data[8:]
	return v
}

func (p *packetReader) readStr() (string, error) {
	n := p.readU32()
	if p.err != nil {
		return "", p.err
	}
	if uint32(len(p.data)) < n {
		p.err = fmt.Errorf("sftp: short packet (string needs %d bytes, have %d)", n, len(p.data))
		return "", p.err
	}
	s := string(p.data[:n])
	p.data = p.data[n:]
	return s, nil
}

// readAttrsSize reads an SFTP ATTRS struct and returns the file size.
// Returns -1 if the size flag is absent (e.g. for directories without size).
func (p *packetReader) readAttrsSize() (int64, error) {
	flags := p.readU32()
	if p.err != nil {
		return -1, p.err
	}
	if flags&sftpAttrSize != 0 {
		return int64(p.readU64()), p.err
	}
	return -1, p.err
}

// ─── sftpReader ───────────────────────────────────────────────────────────────

// sftpReader implements RemoteReader for sftp:// and scp:// URIs.
// It opens a new SSH connection and SFTP subsystem channel per operation.
// scp:// URIs are treated identically to sftp:// at the protocol level.
type sftpReader struct {
	host        string // host:port
	user        string
	authMethods []ssh.AuthMethod
	hostKeyFP   string // expected SHA-256 fingerprint
}

/** parseSFTPURI extracts the host and optional username from an sftp:// or scp:// URI.
 * The path component is not extracted here; use parseSFTPPath for that.
 *
 * Parameters:
 *   uri (string) — sftp://[user@]host[:port]/path or scp://[user@]host[:port]/path.
 *
 * Returns:
 *   host  (string) — host (and optional port) for the SSH connection.
 *   user  (string) — SSH username; empty when omitted from the URI.
 *   error          — when the URI is malformed or the scheme is not sftp/scp.
 *
 * Example:
 *   host, user, _ := parseSFTPURI("sftp://alice@fileserver.local/data/")
 *   // host="fileserver.local", user="alice"
 */
func parseSFTPURI(uri string) (host, user string, err error) {
	scheme := parseURIScheme(uri)
	if scheme != "sftp" && scheme != "scp" {
		return "", "", fmt.Errorf("sftp: not an sftp/scp URI: %q", uri)
	}
	rest := uri[len(scheme)+3:]
	authority := rest
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		authority = rest[:idx]
	}
	if authority == "" {
		return "", "", fmt.Errorf("sftp: no host in URI: %q", uri)
	}
	if at := strings.IndexByte(authority, '@'); at >= 0 {
		user = authority[:at]
		host = authority[at+1:]
	} else {
		host = authority
	}
	if host == "" {
		return "", "", fmt.Errorf("sftp: no host in URI: %q", uri)
	}
	return host, user, nil
}

/** parseSFTPPath extracts the path component from an sftp:// or scp:// URI.
 * Returns "/" when no path is present.
 *
 * Parameters:
 *   uri (string) — sftp:// or scp:// URI.
 *
 * Returns:
 *   string — the absolute path portion, always starting with '/'.
 *
 * Example:
 *   parseSFTPPath("sftp://host/docs/file.txt") // "/docs/file.txt"
 *   parseSFTPPath("sftp://host")               // "/"
 */
func parseSFTPPath(uri string) string {
	scheme := parseURIScheme(uri)
	if scheme != "sftp" && scheme != "scp" {
		return "/"
	}
	rest := uri[len(scheme)+3:]
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return "/"
	}
	return rest[idx:]
}

/** newSFTPReader constructs an sftpReader from environment variables.
 *
 * Required env var:
 *   SFTP_HOST_KEY — SHA-256 fingerprint of the server's host key
 *                   (e.g. "SHA256:abc..."). Must be set; missing is an error.
 *
 * Optional env vars (at least one must be set for authentication):
 *   SFTP_KEY_PATH — path to a PEM-encoded private key file.
 *   SFTP_PASSWORD — password for password authentication.
 *   SFTP_USER     — SSH username fallback when user is empty.
 *
 * Parameters:
 *   host (string) — "host" or "host:port"; ":22" appended when no port present.
 *   user (string) — SSH username; falls back to SFTP_USER env var if empty.
 *
 * Returns:
 *   *sftpReader — configured reader.
 *   error       — when SFTP_HOST_KEY is absent or no auth method is available.
 *
 * Example:
 *   r, err := newSFTPReader("fileserver.local", "alice")
 */
func newSFTPReader(host, user string) (*sftpReader, error) {
	hostKeyFP := os.Getenv("SFTP_HOST_KEY")
	if hostKeyFP == "" {
		return nil, fmt.Errorf("sftp: SFTP_HOST_KEY must be set (SHA-256 fingerprint of server host key)")
	}

	if user == "" {
		user = os.Getenv("SFTP_USER")
	}

	var auths []ssh.AuthMethod

	if keyPath := os.Getenv("SFTP_KEY_PATH"); keyPath != "" {
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("sftp: reading SFTP_KEY_PATH %q: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("sftp: parsing SFTP_KEY_PATH %q: %w", keyPath, err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	if password := os.Getenv("SFTP_PASSWORD"); password != "" {
		auths = append(auths, ssh.Password(password))
	}

	if len(auths) == 0 {
		return nil, fmt.Errorf("sftp: set SFTP_KEY_PATH or SFTP_PASSWORD for authentication")
	}

	if !strings.Contains(host, ":") {
		host = host + ":22"
	}

	return &sftpReader{
		host:        host,
		user:        user,
		authMethods: auths,
		hostKeyFP:   hostKeyFP,
	}, nil
}

// dial opens a new SSH connection to r.host, verifying the server host key.
func (r *sftpReader) dial() (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: r.user,
		Auth: r.authMethods,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fp := ssh.FingerprintSHA256(key)
			if fp != r.hostKeyFP {
				return fmt.Errorf("sftp: host key mismatch: got %s, want %s", fp, r.hostKeyFP)
			}
			return nil
		},
	}
	return ssh.Dial("tcp", r.host, cfg)
}

// ─── sftpSession ─────────────────────────────────────────────────────────────

// sftpSession wraps an SSH session running the SFTP subsystem.
type sftpSession struct {
	r       io.Reader
	w       io.Writer
	session *ssh.Session
	client  *ssh.Client
	reqID   uint32
}

// newSFTPSession opens an SSH connection, requests the SFTP subsystem, and
// completes the SSH_FXP_INIT/VERSION handshake.
func (r *sftpReader) newSFTPSession() (*sftpSession, error) {
	client, err := r.dial()
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}

	pw, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	pr, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	// Request the SFTP subsystem. Payload is an SSH string: uint32(4) + "sftp".
	ok, err := session.SendRequest("subsystem", true, sftpPutStr("sftp"))
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("sftp: subsystem request: %w", err)
	}
	if !ok {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("sftp: server rejected sftp subsystem request")
	}

	sc := &sftpSession{r: pr, w: pw, session: session, client: client}
	if err := sc.init(); err != nil {
		sc.close()
		return nil, err
	}
	return sc, nil
}

func (sc *sftpSession) close() {
	sc.session.Close()
	sc.client.Close()
}

func (sc *sftpSession) nextID() uint32 {
	sc.reqID++
	return sc.reqID
}

// init sends SSH_FXP_INIT and reads the SSH_FXP_VERSION response.
func (sc *sftpSession) init() error {
	if err := sftpSendPacket(sc.w, sftpFxpInit, sftpPutU32(3)); err != nil {
		return fmt.Errorf("sftp: send INIT: %w", err)
	}
	pktType, _, err := sftpReadPacket(sc.r)
	if err != nil {
		return fmt.Errorf("sftp: read VERSION: %w", err)
	}
	if pktType != sftpFxpVersion {
		return fmt.Errorf("sftp: expected VERSION packet (type 2), got type %d", pktType)
	}
	return nil
}

// stat sends SSH_FXP_STAT and returns the file size from ATTRS.
func (sc *sftpSession) stat(path string) (int64, error) {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(path)...)
	if err := sftpSendPacket(sc.w, sftpFxpStat, payload); err != nil {
		return -1, err
	}
	pktType, data, err := sftpReadPacket(sc.r)
	if err != nil {
		return -1, err
	}
	pr := &packetReader{data: data}
	_ = pr.readU32() // reqID
	switch pktType {
	case sftpFxpAttrs:
		return pr.readAttrsSize()
	case sftpFxpStatus:
		code := pr.readU32()
		msg, _ := pr.readStr()
		return -1, fmt.Errorf("sftp: stat %q: status %d: %s", path, code, msg)
	default:
		return -1, fmt.Errorf("sftp: stat: unexpected packet type %d", pktType)
	}
}

// open sends SSH_FXP_OPEN for reading and returns the server handle string.
func (sc *sftpSession) open(path string) (string, error) {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(path)...)
	payload = append(payload, sftpPutU32(sftpOpenRead)...)
	payload = append(payload, sftpPutU32(0)...) // empty ATTRS
	if err := sftpSendPacket(sc.w, sftpFxpOpen, payload); err != nil {
		return "", err
	}
	pktType, data, err := sftpReadPacket(sc.r)
	if err != nil {
		return "", err
	}
	pr := &packetReader{data: data}
	_ = pr.readU32() // reqID
	switch pktType {
	case sftpFxpHandle:
		return pr.readStr()
	case sftpFxpStatus:
		code := pr.readU32()
		msg, _ := pr.readStr()
		return "", fmt.Errorf("sftp: open %q: status %d: %s", path, code, msg)
	default:
		return "", fmt.Errorf("sftp: open: unexpected packet type %d", pktType)
	}
}

// read sends SSH_FXP_READ for one chunk at the given offset.
// Returns (chunk, eof, err). eof is true when the server returns SSH_FX_EOF.
func (sc *sftpSession) read(handle string, offset uint64, maxLen uint32) ([]byte, bool, error) {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(handle)...)
	payload = append(payload, sftpPutU64(offset)...)
	payload = append(payload, sftpPutU32(maxLen)...)
	if err := sftpSendPacket(sc.w, sftpFxpRead, payload); err != nil {
		return nil, false, err
	}
	pktType, data, err := sftpReadPacket(sc.r)
	if err != nil {
		return nil, false, err
	}
	pr := &packetReader{data: data}
	_ = pr.readU32() // reqID
	switch pktType {
	case sftpFxpData:
		chunk, err := pr.readStr()
		return []byte(chunk), false, err
	case sftpFxpStatus:
		code := pr.readU32()
		if code == sftpStatusEOF {
			return nil, true, nil
		}
		msg, _ := pr.readStr()
		return nil, false, fmt.Errorf("sftp: read: status %d: %s", code, msg)
	default:
		return nil, false, fmt.Errorf("sftp: read: unexpected packet type %d", pktType)
	}
}

// closeHandle sends SSH_FXP_CLOSE and discards the status response.
func (sc *sftpSession) closeHandle(handle string) error {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(handle)...)
	if err := sftpSendPacket(sc.w, sftpFxpClose, payload); err != nil {
		return err
	}
	_, _, err := sftpReadPacket(sc.r)
	return err
}

// opendir sends SSH_FXP_OPENDIR and returns the server handle string.
func (sc *sftpSession) opendir(path string) (string, error) {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(path)...)
	if err := sftpSendPacket(sc.w, sftpFxpOpendir, payload); err != nil {
		return "", err
	}
	pktType, data, err := sftpReadPacket(sc.r)
	if err != nil {
		return "", err
	}
	pr := &packetReader{data: data}
	_ = pr.readU32() // reqID
	switch pktType {
	case sftpFxpHandle:
		return pr.readStr()
	case sftpFxpStatus:
		code := pr.readU32()
		msg, _ := pr.readStr()
		return "", fmt.Errorf("sftp: opendir %q: status %d: %s", path, code, msg)
	default:
		return "", fmt.Errorf("sftp: opendir: unexpected packet type %d", pktType)
	}
}

// sftpDirEntry holds a single entry returned by SSH_FXP_NAME.
type sftpDirEntry struct {
	name string
	size int64
}

// readdir sends SSH_FXP_READDIR.
// Returns (entries, done, err); done=true when the server signals EOF.
func (sc *sftpSession) readdir(handle string) ([]sftpDirEntry, bool, error) {
	id := sc.nextID()
	payload := append(sftpPutU32(id), sftpPutStr(handle)...)
	if err := sftpSendPacket(sc.w, sftpFxpReaddir, payload); err != nil {
		return nil, false, err
	}
	pktType, data, err := sftpReadPacket(sc.r)
	if err != nil {
		return nil, false, err
	}
	pr := &packetReader{data: data}
	_ = pr.readU32() // reqID
	switch pktType {
	case sftpFxpName:
		count := pr.readU32()
		entries := make([]sftpDirEntry, 0, count)
		for i := uint32(0); i < count; i++ {
			name, _ := pr.readStr()
			_, _ = pr.readStr() // long name (ls -l format)
			size, _ := pr.readAttrsSize()
			entries = append(entries, sftpDirEntry{name: name, size: size})
		}
		return entries, false, pr.err
	case sftpFxpStatus:
		code := pr.readU32()
		if code == sftpStatusEOF {
			return nil, true, nil
		}
		msg, _ := pr.readStr()
		return nil, false, fmt.Errorf("sftp: readdir: status %d: %s", code, msg)
	default:
		return nil, false, fmt.Errorf("sftp: readdir: unexpected packet type %d", pktType)
	}
}

// ─── RemoteReader implementation ─────────────────────────────────────────────

/** Stat returns metadata for the remote file at uri.
 *
 * Parameters:
 *   ctx (context.Context) — unused; present for interface compliance.
 *   uri (string)          — sftp:// or scp:// URI.
 *
 * Returns:
 *   RemoteObjectInfo — URI, Size (from ATTRS); ContentType is always empty.
 *   error            — on connection or protocol failure.
 *
 * Example:
 *   info, err := r.Stat(ctx, "sftp://host/file.txt")
 */
func (r *sftpReader) Stat(_ context.Context, uri string) (RemoteObjectInfo, error) {
	path := parseSFTPPath(uri)
	sc, err := r.newSFTPSession()
	if err != nil {
		return RemoteObjectInfo{}, err
	}
	defer sc.close()

	size, err := sc.stat(path)
	if err != nil {
		return RemoteObjectInfo{}, err
	}
	return RemoteObjectInfo{URI: uri, Size: size}, nil
}

/** Get downloads the remote file at uri and writes its content to dst.
 *
 * Parameters:
 *   ctx (context.Context) — unused; present for interface compliance.
 *   uri (string)          — sftp:// or scp:// URI.
 *   dst (io.Writer)       — receives the downloaded bytes.
 *
 * Returns:
 *   error — on connection, protocol, or write failure.
 *
 * Example:
 *   var buf bytes.Buffer
 *   err := r.Get(ctx, "sftp://host/file.txt", &buf)
 */
func (r *sftpReader) Get(_ context.Context, uri string, dst io.Writer) error {
	path := parseSFTPPath(uri)
	sc, err := r.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.close()

	handle, err := sc.open(path)
	if err != nil {
		return err
	}
	defer sc.closeHandle(handle) //nolint:errcheck

	const chunkSize = uint32(32768)
	var offset uint64
	for {
		chunk, eof, err := sc.read(handle, offset, chunkSize)
		if err != nil {
			return err
		}
		if len(chunk) > 0 {
			if _, err := dst.Write(chunk); err != nil {
				return err
			}
			offset += uint64(len(chunk))
		}
		if eof {
			return nil
		}
	}
}

/** List returns all entries under the prefix URI (must end with '/').
 * The URI's path component is treated as a directory.
 *
 * Parameters:
 *   ctx    (context.Context) — unused; present for interface compliance.
 *   prefix (string)          — sftp:// or scp:// URI ending with '/'.
 *
 * Returns:
 *   []RemoteObjectInfo — one entry per directory item; '.' and '..' are skipped.
 *   error              — on connection or protocol failure.
 *
 * Example:
 *   items, err := r.List(ctx, "sftp://host/docs/")
 */
func (r *sftpReader) List(_ context.Context, prefix string) ([]RemoteObjectInfo, error) {
	path := parseSFTPPath(prefix)
	dirPath := strings.TrimRight(path, "/")
	if dirPath == "" {
		dirPath = "/"
	}

	sc, err := r.newSFTPSession()
	if err != nil {
		return nil, err
	}
	defer sc.close()

	handle, err := sc.opendir(dirPath)
	if err != nil {
		return nil, err
	}
	defer sc.closeHandle(handle) //nolint:errcheck

	// Reconstruct the authority portion of the URI for building entry URIs.
	scheme := parseURIScheme(prefix)
	rest := prefix[len(scheme)+3:]
	authority := rest
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		authority = rest[:idx]
	}
	baseURI := scheme + "://" + authority

	var items []RemoteObjectInfo
	for {
		entries, done, err := sc.readdir(handle)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		for _, e := range entries {
			if e.name == "." || e.name == ".." {
				continue
			}
			items = append(items, RemoteObjectInfo{
				URI:  baseURI + dirPath + "/" + e.name,
				Size: e.size,
			})
		}
	}
	return items, nil
}
