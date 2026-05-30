package harvey

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// ─── parseSFTPURI unit tests ──────────────────────────────────────────────────

func TestParseSFTPURI_withUser(t *testing.T) {
	host, user, err := parseSFTPURI("sftp://alice@fileserver.local/data/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "fileserver.local" {
		t.Errorf("host: got %q, want %q", host, "fileserver.local")
	}
	if user != "alice" {
		t.Errorf("user: got %q, want %q", user, "alice")
	}
}

func TestParseSFTPURI_withoutUser(t *testing.T) {
	host, user, err := parseSFTPURI("sftp://fileserver.local/data/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "fileserver.local" {
		t.Errorf("host: got %q, want %q", host, "fileserver.local")
	}
	if user != "" {
		t.Errorf("user: got %q, want empty", user)
	}
}

func TestParseSFTPURI_scpScheme(t *testing.T) {
	host, user, err := parseSFTPURI("scp://bob@storage.local/backups/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "storage.local" {
		t.Errorf("host: got %q, want %q", host, "storage.local")
	}
	if user != "bob" {
		t.Errorf("user: got %q, want %q", user, "bob")
	}
}

func TestParseSFTPURI_noHost_error(t *testing.T) {
	_, _, err := parseSFTPURI("sftp:///path")
	if err == nil {
		t.Fatal("expected error for URI with no host, got nil")
	}
}

func TestParseSFTPURI_wrongScheme_error(t *testing.T) {
	_, _, err := parseSFTPURI("http://example.com/file")
	if err == nil {
		t.Fatal("expected error for non-sftp/scp URI, got nil")
	}
}

// ─── newSFTPReader env var tests ──────────────────────────────────────────────

func TestNewSFTPReader_noHostKey_error(t *testing.T) {
	t.Setenv("SFTP_HOST_KEY", "")
	_, err := newSFTPReader("localhost", "user")
	if err == nil {
		t.Fatal("expected error when SFTP_HOST_KEY is not set")
	}
	if !strings.Contains(err.Error(), "SFTP_HOST_KEY") {
		t.Errorf("error should mention SFTP_HOST_KEY: %v", err)
	}
}

func TestNewSFTPReader_noAuth_error(t *testing.T) {
	t.Setenv("SFTP_HOST_KEY", "SHA256:aaaa")
	t.Setenv("SFTP_KEY_PATH", "")
	t.Setenv("SFTP_PASSWORD", "")
	_, err := newSFTPReader("localhost", "user")
	if err == nil {
		t.Fatal("expected error when neither SFTP_KEY_PATH nor SFTP_PASSWORD are set")
	}
}

// ─── minimal in-process SSH+SFTP test server ─────────────────────────────────

// sftpTestAttr builds a minimal SFTP ATTRS with only size set.
func sftpTestAttr(size uint64) []byte {
	b := sftpPutU32(sftpAttrSize)
	b = append(b, sftpPutU64(size)...)
	return b
}

// sftpTestStatus builds a STATUS response payload.
func sftpTestStatus(reqID, code uint32, msg string) []byte {
	b := sftpPutU32(reqID)
	b = append(b, sftpPutU32(code)...)
	b = append(b, sftpPutStr(msg)...)
	b = append(b, sftpPutStr("en")...)
	return b
}

// sftpTestHandle builds a HANDLE response payload.
func sftpTestHandle(reqID uint32, handle string) []byte {
	b := sftpPutU32(reqID)
	b = append(b, sftpPutStr(handle)...)
	return b
}

// sftpServe is the test SFTP subsystem handler. It speaks SFTP v3 over rw.
// Serves one file (/test.txt = "hello from sftp") and one directory (/testdir)
// with two entries: alpha.md and beta.txt.
func sftpServe(rw io.ReadWriter) {
	const fileContent = "hello from sftp"
	type dirEntry struct {
		name string
		size uint64
	}
	dirEntries := []dirEntry{
		{"alpha.md", 10},
		{"beta.txt", 20},
	}

	type handleState struct {
		kind   string // "file" or "dir"
		offset uint64
		listed bool
	}
	handles := map[string]*handleState{}
	nextHandle := 0

	newHandle := func(kind string) string {
		nextHandle++
		h := kind + "Handle"
		if nextHandle > 1 {
			h += string(rune('0' + nextHandle))
		}
		handles[h] = &handleState{kind: kind}
		return h
	}

	for {
		pktType, payload, err := sftpReadPacket(rw)
		if err != nil {
			return
		}
		pr := bytes.NewReader(payload)

		switch pktType {
		case sftpFxpInit:
			// payload = uint32(version); no reqID
			sftpSendPacket(rw, sftpFxpVersion, sftpPutU32(3))

		case sftpFxpStat:
			reqID, _ := sftpReadU32(pr)
			path, _ := sftpReadStr(pr)
			switch path {
			case "/test.txt":
				b := sftpPutU32(reqID)
				b = append(b, sftpTestAttr(uint64(len(fileContent)))...)
				sftpSendPacket(rw, sftpFxpAttrs, b)
			case "/testdir", "/testdir/":
				b := sftpPutU32(reqID)
				b = append(b, sftpPutU32(0x00000004)...) // permissions flag only
				b = append(b, sftpPutU32(0o40755)...)
				sftpSendPacket(rw, sftpFxpAttrs, b)
			default:
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, 2, "no such file"))
			}

		case sftpFxpOpen:
			reqID, _ := sftpReadU32(pr)
			path, _ := sftpReadStr(pr)
			if path != "/test.txt" {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, 2, "no such file"))
				continue
			}
			h := newHandle("file")
			sftpSendPacket(rw, sftpFxpHandle, sftpTestHandle(reqID, h))

		case sftpFxpRead:
			reqID, _ := sftpReadU32(pr)
			handle, _ := sftpReadStr(pr)
			offset, _ := sftpReadU64(pr)
			length, _ := sftpReadU32(pr)
			hs, ok := handles[handle]
			if !ok || hs.kind != "file" {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, 4, "invalid handle"))
				continue
			}
			content := []byte(fileContent)
			if offset >= uint64(len(content)) {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, sftpStatusEOF, "EOF"))
				continue
			}
			end := offset + uint64(length)
			if end > uint64(len(content)) {
				end = uint64(len(content))
			}
			chunk := content[offset:end]
			b := sftpPutU32(reqID)
			b = append(b, sftpPutStr(string(chunk))...)
			sftpSendPacket(rw, sftpFxpData, b)

		case sftpFxpClose:
			reqID, _ := sftpReadU32(pr)
			handle, _ := sftpReadStr(pr)
			delete(handles, handle)
			sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, sftpStatusOK, "ok"))

		case sftpFxpOpendir:
			reqID, _ := sftpReadU32(pr)
			path, _ := sftpReadStr(pr)
			if path != "/testdir" && path != "/testdir/" {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, 2, "no such directory"))
				continue
			}
			h := newHandle("dir")
			sftpSendPacket(rw, sftpFxpHandle, sftpTestHandle(reqID, h))

		case sftpFxpReaddir:
			reqID, _ := sftpReadU32(pr)
			handle, _ := sftpReadStr(pr)
			hs, ok := handles[handle]
			if !ok || hs.kind != "dir" {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, 4, "invalid handle"))
				continue
			}
			if hs.listed {
				sftpSendPacket(rw, sftpFxpStatus, sftpTestStatus(reqID, sftpStatusEOF, "EOF"))
				continue
			}
			hs.listed = true
			b := sftpPutU32(reqID)
			b = append(b, sftpPutU32(uint32(len(dirEntries)))...)
			for _, e := range dirEntries {
				b = append(b, sftpPutStr(e.name)...)
				b = append(b, sftpPutStr("-rw-r--r-- 1 u g 0 Jan 1 00:00 "+e.name)...)
				b = append(b, sftpTestAttr(e.size)...)
			}
			sftpSendPacket(rw, sftpFxpName, b)
		}
	}
}

/** fakeSFTPServer starts a minimal in-process SSH+SFTP test server.
 *
 * Parameters:
 *   t (*testing.T) — test instance; server is cleaned up via t.Cleanup.
 *
 * Returns:
 *   addr        (string) — "host:port" the server is listening on.
 *   fingerprint (string) — SHA-256 fingerprint of the server's host key.
 *
 * Example:
 *   addr, fp := fakeSFTPServer(t)
 *   t.Setenv("SFTP_HOST_KEY", fp)
 */
func fakeSFTPServer(t *testing.T) (addr, fingerprint string) {
	t.Helper()

	_, hostPrivKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	fingerprint = ssh.FingerprintSHA256(hostSigner.PublicKey())

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	addr = ln.Addr().String()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSFTPConn(conn, cfg)
		}
	}()
	return addr, fingerprint
}

func serveSFTPConn(conn net.Conn, cfg *ssh.ServerConfig) {
	srvConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	defer srvConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			return
		}
		go serveSFTPSession(ch, requests)
	}
}

func serveSFTPSession(ch ssh.Channel, requests <-chan *ssh.Request) {
	defer ch.Close()
	for req := range requests {
		if req.Type == "subsystem" {
			pr := bytes.NewReader(req.Payload)
			_, _ = sftpReadStr(pr) // subsystem name (we only accept sftp)
			// Peek at name without advancing — re-read from raw payload
			if len(req.Payload) >= 4 {
				nameLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 |
					int(req.Payload[2])<<8 | int(req.Payload[3])
				if nameLen == 4 && len(req.Payload) >= 8 &&
					string(req.Payload[4:8]) == "sftp" {
					req.Reply(true, nil)
					sftpServe(ch)
					return
				}
			}
		}
		if req.WantReply {
			req.Reply(false, nil)
		}
	}
}

// ─── SFTP integration tests ───────────────────────────────────────────────────

func TestSFTPReader_Get(t *testing.T) {
	addr, fp := fakeSFTPServer(t)
	t.Setenv("SFTP_HOST_KEY", fp)
	t.Setenv("SFTP_PASSWORD", "anypass")
	t.Setenv("SFTP_KEY_PATH", "")

	r, err := newSFTPReader(addr, "testuser")
	if err != nil {
		t.Fatalf("newSFTPReader: %v", err)
	}
	var buf bytes.Buffer
	if err := r.Get(context.Background(), "sftp://"+addr+"/test.txt", &buf); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if buf.String() != "hello from sftp" {
		t.Errorf("content: got %q, want %q", buf.String(), "hello from sftp")
	}
}

func TestSFTPReader_Stat(t *testing.T) {
	addr, fp := fakeSFTPServer(t)
	t.Setenv("SFTP_HOST_KEY", fp)
	t.Setenv("SFTP_PASSWORD", "anypass")
	t.Setenv("SFTP_KEY_PATH", "")

	r, err := newSFTPReader(addr, "testuser")
	if err != nil {
		t.Fatalf("newSFTPReader: %v", err)
	}
	info, err := r.Stat(context.Background(), "sftp://"+addr+"/test.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	want := int64(len("hello from sftp"))
	if info.Size != want {
		t.Errorf("Size: got %d, want %d", info.Size, want)
	}
	if info.URI != "sftp://"+addr+"/test.txt" {
		t.Errorf("URI: got %q", info.URI)
	}
}

func TestSFTPReader_List(t *testing.T) {
	addr, fp := fakeSFTPServer(t)
	t.Setenv("SFTP_HOST_KEY", fp)
	t.Setenv("SFTP_PASSWORD", "anypass")
	t.Setenv("SFTP_KEY_PATH", "")

	r, err := newSFTPReader(addr, "testuser")
	if err != nil {
		t.Fatalf("newSFTPReader: %v", err)
	}
	items, err := r.List(context.Background(), "sftp://"+addr+"/testdir/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List: got %d items, want 2", len(items))
	}
	if !strings.HasSuffix(items[0].URI, "alpha.md") {
		t.Errorf("items[0].URI: got %q, want suffix alpha.md", items[0].URI)
	}
	if !strings.HasSuffix(items[1].URI, "beta.txt") {
		t.Errorf("items[1].URI: got %q, want suffix beta.txt", items[1].URI)
	}
}

func TestSFTPReader_wrongFingerprint_error(t *testing.T) {
	addr, _ := fakeSFTPServer(t)
	t.Setenv("SFTP_HOST_KEY", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("SFTP_PASSWORD", "anypass")
	t.Setenv("SFTP_KEY_PATH", "")

	r, err := newSFTPReader(addr, "testuser")
	if err != nil {
		t.Fatalf("newSFTPReader: %v", err)
	}
	var buf bytes.Buffer
	err = r.Get(context.Background(), "sftp://"+addr+"/test.txt", &buf)
	if err == nil {
		t.Fatal("expected error for wrong host key fingerprint, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error should mention mismatch: %v", err)
	}
}
