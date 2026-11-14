package imap

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"one-mail/backend/internal/models"
)

func TestSelectBodyParts(t *testing.T) {
	t.Run("single text/plain part", func(t *testing.T) {
		bs := &goimap.BodyStructureSinglePart{
			Type:    "text",
			Subtype: "plain",
		}
		sel := selectBodyParts(bs)
		if sel.isMultipart {
			t.Fatal("expected non-multipart")
		}
		if !reflect.DeepEqual(sel.textPath, []int{1}) || len(sel.htmlPath) != 0 {
			t.Fatalf("unexpected paths: %+v", sel)
		}
	})

	t.Run("single attachment only skips download", func(t *testing.T) {
		bs := &goimap.BodyStructureSinglePart{
			Type:    "application",
			Subtype: "pdf",
			Extended: &goimap.BodyStructureSinglePartExt{
				Disposition: &goimap.BodyStructureDisposition{Value: "attachment"},
			},
		}
		sel := selectBodyParts(bs)
		if len(sel.textPath) != 0 || len(sel.htmlPath) != 0 {
			t.Fatalf("expected no text/html paths for pdf, got %+v", sel)
		}
	})

	t.Run("multipart/mixed with alternative and attachment selects only body parts", func(t *testing.T) {
		bs := &goimap.BodyStructureMultiPart{
			Subtype: "mixed",
			Children: []goimap.BodyStructure{
				&goimap.BodyStructureMultiPart{
					Subtype: "alternative",
					Children: []goimap.BodyStructure{
						&goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
						&goimap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
					},
				},
				&goimap.BodyStructureSinglePart{
					Type:    "application",
					Subtype: "zip",
					Size:    10 * 1024 * 1024,
					Extended: &goimap.BodyStructureSinglePartExt{
						Disposition: &goimap.BodyStructureDisposition{Value: "attachment"},
					},
				},
				// Inline text/plain with attachment disposition should also be ignored
				&goimap.BodyStructureSinglePart{
					Type:    "text",
					Subtype: "plain",
					Extended: &goimap.BodyStructureSinglePartExt{
						Disposition: &goimap.BodyStructureDisposition{Value: "attachment"},
					},
				},
			},
		}
		sel := selectBodyParts(bs)
		if !sel.isMultipart {
			t.Fatal("expected multipart")
		}
		if !reflect.DeepEqual(sel.textPath, []int{1, 1}) {
			t.Fatalf("expected textPath [1 1], got %v", sel.textPath)
		}
		if !reflect.DeepEqual(sel.htmlPath, []int{1, 2}) {
			t.Fatalf("expected htmlPath [1 2], got %v", sel.htmlPath)
		}
	})
}

func TestDecodeBodyPart(t *testing.T) {
	original := "你好，这是一封测试邮件正文！"
	encoded := base64.StdEncoding.EncodeToString([]byte(original))
	mimeHeader := []byte("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n")

	decoded, err := decodeBodyPart(mimeHeader, []byte(encoded))
	if err != nil || decoded != original {
		t.Fatalf("expected %q, got %q, err=%v", original, decoded, err)
	}

	qpHeader := []byte("Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qpBody := []byte("<p>Hello=20World</p>")
	if got, err := decodeBodyPart(qpHeader, qpBody); err != nil || got != "<p>Hello World</p>" {
		t.Fatalf("expected decoded quoted-printable, got %q, err=%v", got, err)
	}
}

func TestComputeLatestSeqRange(t *testing.T) {
	start, end, ok := computeLatestSeqRange(0, 50)
	if ok || start != 0 || end != 0 {
		t.Fatalf("expected empty mailbox (0,0,false), got (%d,%d,%v)", start, end, ok)
	}

	start, end, ok = computeLatestSeqRange(12, 50)
	if !ok || start != 1 || end != 12 {
		t.Fatalf("expected (1,12,true), got (%d,%d,%v)", start, end, ok)
	}

	start, end, ok = computeLatestSeqRange(200, 50)
	if !ok || start != 151 || end != 200 {
		t.Fatalf("expected (151,200,true), got (%d,%d,%v)", start, end, ok)
	}

	start, end, ok = computeLatestSeqRange(50, 0)
	if !ok || start != 1 || end != 50 {
		t.Fatalf("expected default limit 50 -> (1,50,true), got (%d,%d,%v)", start, end, ok)
	}
}

func TestConnectContext_CancelsStalledServerAndAuthErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	doneAccept := make(chan struct{})
	go func() {
		defer close(doneAccept)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(2 * time.Second)
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	client := NewClient(&models.EmailAccount{
		IMAPHost: "127.0.0.1",
		IMAPPort: port,
		Username: "u",
		Password: "p",
	}, NewConnectionPool())

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = client.ConnectContext(ctx)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected ConnectContext to fail on stalled server")
	}
	if elapsed > time.Second {
		t.Fatalf("expected ConnectContext to cancel promptly, took %v", elapsed)
	}
	<-doneAccept

	if !IsAuthError(&goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAuthenticationFailed, Text: "Login fail."}) {
		t.Fatal("expected IsAuthError to match QQ AUTHENTICATIONFAILED")
	}
	if IsAuthError(errors.New("i/o timeout")) {
		t.Fatal("did not expect IsAuthError to match i/o timeout")
	}
}

func scriptedClient(t *testing.T, respond func(string, string) string) *Client {
	t.Helper()
	local, remote := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer remote.Close()
		if _, err := fmt.Fprint(remote, "* PREAUTH [CAPABILITY IMAP4rev1] test ready\r\n"); err != nil {
			return
		}
		reader := bufio.NewReader(remote)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			tag, command, ok := strings.Cut(strings.TrimSpace(line), " ")
			if !ok {
				return
			}
			response := respond(tag, command)
			if _, err := fmt.Fprint(remote, response); err != nil {
				return
			}
		}
	}()
	client := &Client{client: imapclient.New(local, nil), transport: local}
	t.Cleanup(func() {
		client.ForceClose()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("scripted IMAP peer leaked")
		}
		_ = client.client.Close()
	})
	return client
}

func mailboxResponse(tag string, epoch uint32, count int) string {
	return fmt.Sprintf("* %d EXISTS\r\n* OK [UIDVALIDITY %d] epoch\r\n%s OK selected\r\n", count, epoch, tag)
}

func TestFetchLatestEmails_RetryExhaustionReturnsFinalError(t *testing.T) {
	var attempts atomic.Int32
	client := scriptedClient(t, func(tag, command string) string {
		attempts.Add(1)
		return tag + " NO temporarily unavailable\r\n"
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	emails, epoch, err := client.FetchLatestEmails(ctx, "INBOX", 50)
	if err == nil || !strings.Contains(err.Error(), "temporarily unavailable") || emails != nil || epoch != 0 || attempts.Load() != 3 {
		t.Fatalf("retry exhaustion fabricated success: emails=%v epoch=%d err=%v attempts=%d", emails, epoch, err, attempts.Load())
	}
}

func TestFetchLatestEmails_CancelsRetryAndReportsEmptyMailboxEpoch(t *testing.T) {
	t.Run("cancel retry wait", func(t *testing.T) {
		client := scriptedClient(t, func(tag, command string) string { return tag + " NO system busy\r\n" })
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, _, err := client.FetchLatestEmails(ctx, "INBOX", 50)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
			t.Fatalf("retry wait not cancelled: %v", err)
		}
	})
	t.Run("empty mailbox epoch", func(t *testing.T) {
		client := scriptedClient(t, func(tag, command string) string { return mailboxResponse(tag, 44, 0) })
		emails, epoch, err := client.FetchLatestEmails(context.Background(), "INBOX", 50)
		if err != nil || len(emails) != 0 || epoch != 44 {
			t.Fatalf("empty mailbox lost epoch: %v %d %v", emails, epoch, err)
		}
	})
}

func TestFetchEmailBody_RejectsEpochBeforeFetchingUID(t *testing.T) {
	var commands atomic.Int32
	client := scriptedClient(t, func(tag, command string) string { commands.Add(1); return mailboxResponse(tag, 2, 1) })
	_, _, err := client.FetchEmailBody(context.Background(), "INBOX", 7, 1)
	if !errors.Is(err, ErrUIDValidityChanged) || commands.Load() != 1 {
		t.Fatalf("body used UID from wrong epoch: %v commands=%d", err, commands.Load())
	}
}

func TestFetchEmailBody_RejectsOversizedLiteralAndUnblocks(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		if strings.HasPrefix(command, "EXAMINE") {
			return mailboxResponse(tag, 1, 1)
		}
		if strings.Contains(command, "BODYSTRUCTURE") {
			return "* 1 FETCH (UID 7)\r\n" + tag + " OK structure\r\n"
		}
		body := strings.Repeat("x", maxBodyBytes+1)
		return fmt.Sprintf("* 1 FETCH (UID 7 BODY[]<0> {%d}\r\n%s)\r\n%s OK body\r\n", len(body), body, tag)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := client.FetchEmailBody(ctx, "INBOX", 7, 1)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("body limit not enforced: %v", err)
	}
}

func TestFetchEmailBody_UsesExamineAndPeekWithoutStore(t *testing.T) {
	for _, structure := range []string{
		"",
		`("TEXT" "PLAIN" ("CHARSET" "UTF-8") NIL NIL "7BIT" 5 1 NIL NIL NIL NIL)`,
	} {
		t.Run(structure, func(t *testing.T) {
			var examined, peeked atomic.Int32
			client := scriptedClient(t, func(tag, command string) string {
				if strings.HasPrefix(command, "EXAMINE ") {
					examined.Add(1)
					return mailboxResponse(tag, 1, 1)
				}
				if strings.Contains(command, "BODYSTRUCTURE") {
					if structure == "" {
						return "* 1 FETCH (UID 7)\r\n" + tag + " OK structure\r\n"
					}
					return "* 1 FETCH (UID 7 BODYSTRUCTURE " + structure + ")\r\n" + tag + " OK structure\r\n"
				}
				if strings.Contains(command, "BODY.PEEK[]") {
					peeked.Add(1)
					raw := "Content-Type: text/plain; charset=utf-8\r\n\r\nhello"
					return fmt.Sprintf("* 1 FETCH (UID 7 BODY[]<0> {%d}\r\n%s)\r\n%s OK body\r\n", len(raw), raw, tag)
				}
				t.Errorf("unexpected command; viewing must be read-only: %s", command)
				return tag + " BAD unexpected command\r\n"
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			text, html, err := client.FetchEmailBody(ctx, "INBOX", 7, 1)
			if err != nil || text != "hello" || html != "" || examined.Load() != 1 || peeked.Load() != 1 {
				t.Fatalf("read-only body fetch failed: text=%q html=%q err=%v examine=%d peek=%d", text, html, err, examined.Load(), peeked.Load())
			}
		})
	}
}

func TestFetchLatestEmails_DoesNotRequestFlags(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		if strings.HasPrefix(command, "EXAMINE ") {
			return mailboxResponse(tag, 3, 1)
		}
		if strings.HasPrefix(command, "FETCH ") && strings.Contains(command, "ENVELOPE") && strings.Contains(command, "BODYSTRUCTURE") && !strings.Contains(command, "FLAGS") && !strings.Contains(command, "BODY.PEEK") {
			return "* 1 FETCH (UID 7)\r\n" + tag + " OK summary\r\n"
		}
		t.Errorf("unexpected summary command: %s", command)
		return tag + " BAD unexpected command\r\n"
	})
	emails, epoch, err := client.FetchLatestEmails(context.Background(), "INBOX", 50)
	if err != nil || epoch != 3 || len(emails) != 1 || emails[0].UID != 7 {
		t.Fatalf("summary fetch failed: %v epoch=%d err=%v", emails, epoch, err)
	}
}

func TestParseMessageBody_RejectsMalformedMIMEWithoutPartialBody(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		text string
		html string
		fail bool
	}{
		{name: "empty plain body", raw: "Content-Type: text/plain\r\n\r\n"},
		{name: "empty html body", raw: "Content-Type: text/html\r\n\r\n"},
		{name: "default content type", raw: "Subject: Empty\r\n\r\nhello", text: "hello"},
		{name: "missing message", fail: true},
		{name: "invalid header", raw: "invalid header\r\n\r\nhello", fail: true},
		{name: "invalid content type", raw: "Content-Type: text/plain; charset=\"\r\n\r\nhello", fail: true},
		{name: "missing boundary", raw: "Content-Type: multipart/mixed\r\n\r\nhello", fail: true},
		{name: "truncated multipart", raw: "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\npartial", fail: true},
		{name: "broken base64", raw: "Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=!!!!", fail: true},
		{name: "unknown encoding", raw: "Content-Type: text/plain\r\nContent-Transfer-Encoding: x-broken\r\n\r\nencoded", fail: true},
		{name: "unknown charset UTF-8", raw: "Content-Type: text/plain; charset=x-unknown-charset\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=", text: "hello"},
		{name: "unknown charset invalid bytes", raw: "Content-Type: text/plain; charset=x-unknown-charset\r\n\r\n\xff", fail: true},
		{name: "unknown charset cannot mask encoding", raw: "Content-Type: text/plain; charset=x-unknown-charset\r\nContent-Transfer-Encoding: x-broken\r\n\r\nencoded", fail: true},
		{name: "later part fails", raw: "Content-Type: multipart/alternative; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nhello\r\n--b\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\n!!!!\r\n--b--\r\n", fail: true},
		{name: "inline file is not body", raw: "Content-Type: text/plain\r\nContent-Disposition: inline; filename=file.txt\r\n\r\nattachment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, html, err := parseMessageBody([]byte(tt.raw))
			if (err != nil) != tt.fail || text != tt.text || html != tt.html {
				t.Fatalf("text=%q html=%q err=%v; want text=%q html=%q failure=%v", text, html, err, tt.text, tt.html, tt.fail)
			}
		})
	}
}

func TestDecodeBodyPart_EmptyAndMalformedSections(t *testing.T) {
	for _, tt := range []struct {
		name   string
		header string
		body   []byte
		want   string
		fail   bool
	}{
		{name: "valid empty body", header: "Content-Type: text/plain\r\n", body: []byte{}},
		{name: "missing body section", header: "Content-Type: text/plain\r\n", fail: true},
		{name: "missing header", body: []byte("encoded"), fail: true},
		{name: "malformed header with empty body", header: "broken header\r\n", body: []byte{}, fail: true},
		{name: "malformed content type", header: "Content-Type: text/plain; charset=\"\r\n", body: []byte("x"), fail: true},
		{name: "broken base64", header: "Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n", body: []byte("aGVsbG8=!!!!"), fail: true},
		{name: "unknown encoding", header: "Content-Type: text/plain\r\nContent-Transfer-Encoding: x-broken\r\n", body: []byte("raw"), fail: true},
		{name: "unknown charset UTF-8", header: "Content-Type: text/plain; charset=x-unknown-charset\r\n", body: []byte("你好"), want: "你好"},
		{name: "unknown charset invalid bytes", header: "Content-Type: text/plain; charset=x-unknown-charset\r\n", body: []byte{0xff}, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBodyPart([]byte(tt.header), tt.body)
			if (err != nil) != tt.fail || got != tt.want {
				t.Fatalf("got %q, err=%v; want %q failure=%v", got, err, tt.want, tt.fail)
			}
		})
	}
}

func TestIsAuthError_UsesIMAPStatus(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "authentication failed", err: &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAuthenticationFailed}, want: true},
		{name: "wrapped authentication failed", err: fmt.Errorf("failed to login: %w", &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAuthenticationFailed}), want: true},
		{name: "provider without code", err: &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Invalid credentials"}, want: true},
		{name: "temporary NO", err: &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeUnavailable, Text: "Authentication failed temporarily; try again"}},
		{name: "generic NO", err: &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Login failed; server busy"}},
		{name: "BAD", err: &goimap.Error{Type: goimap.StatusResponseTypeBad, Code: goimap.ResponseCodeAuthenticationFailed}},
		{name: "network", err: fmt.Errorf("failed to login: %w", io.ErrUnexpectedEOF)},
		{name: "cancelled", err: fmt.Errorf("failed to login: %w", context.Canceled)},
		{name: "deadline", err: fmt.Errorf("failed to login: %w", context.DeadlineExceeded)},
		{name: "untyped auth-looking error", err: errors.New("failed to login: authentication failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthError(tt.err); got != tt.want {
				t.Fatalf("IsAuthError(%v)=%v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLoginContext_WaitsForGreeting(t *testing.T) {
	local, remote := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer remote.Close()
		reader := bufio.NewReader(remote)
		_ = remote.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if line, err := reader.ReadString('\n'); err == nil {
			tag, _, _ := strings.Cut(line, " ")
			_, _ = fmt.Fprintf(remote, "%s NO LOGIN before greeting\r\n", tag)
			return
		} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			return
		}
		_ = remote.SetReadDeadline(time.Time{})
		if _, err := fmt.Fprint(remote, "* OK [CAPABILITY IMAP4rev1] QQ ready\r\n"); err != nil {
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		tag, command, _ := strings.Cut(strings.TrimSpace(line), " ")
		if !strings.HasPrefix(command, "LOGIN ") {
			t.Errorf("unexpected login command: %s", command)
			return
		}
		_, _ = fmt.Fprintf(remote, "%s OK [CAPABILITY IMAP4rev1] logged in\r\n", tag)
		_, _ = io.Copy(io.Discard, remote)
	}()
	conn := imapclient.New(local, nil)
	t.Cleanup(func() {
		_ = conn.Close()
		<-done
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := loginContext(ctx, conn, "user", "password"); err != nil {
		t.Fatalf("server requiring greeting before LOGIN rejected login: %v", err)
	}
}

func TestLoginContext_CancelsStalledGreeting(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	conn := imapclient.New(local, nil)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loginContext(ctx, conn, "user", "password"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled greeting lost cancellation: %v", err)
	}
}

func TestLoginContext_PreservesServerAndTransportErrors(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response string
		code     goimap.ResponseCode
		auth     bool
	}{
		{name: "invalid credentials", response: "NO [AUTHENTICATIONFAILED] Invalid credentials", code: goimap.ResponseCodeAuthenticationFailed, auth: true},
		{name: "temporarily unavailable", response: "NO [UNAVAILABLE] Server busy", code: goimap.ResponseCodeUnavailable},
		{name: "generic NO", response: "NO Try again later"},
		{name: "transport closed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			local, remote := net.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer remote.Close()
				if _, err := fmt.Fprint(remote, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
					return
				}
				line, err := bufio.NewReader(remote).ReadString('\n')
				if err != nil || tt.response == "" {
					return
				}
				tag, _, _ := strings.Cut(line, " ")
				_, _ = fmt.Fprintf(remote, "%s %s\r\n", tag, tt.response)
			}()
			conn := imapclient.New(local, nil)
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := loginContext(ctx, conn, "user", "password")
			if err == nil || IsAuthError(err) != tt.auth {
				t.Fatalf("login error=%v, auth=%v; want auth=%v", err, IsAuthError(err), tt.auth)
			}
			if tt.response != "" {
				var status *goimap.Error
				if !errors.As(err, &status) || status.Code != tt.code || !strings.Contains(err.Error(), tt.response) {
					t.Fatalf("login discarded server error: %v", err)
				}
			}
			<-done
		})
	}
}

func TestLoginContext_CancelsStalledLogin(t *testing.T) {
	local, remote := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer remote.Close()
		if _, err := fmt.Fprint(remote, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, remote)
	}()
	conn := imapclient.New(local, nil)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := loginContext(ctx, conn, "user", "password"); !errors.Is(err, context.DeadlineExceeded) || IsAuthError(err) {
		t.Fatalf("login cancellation lost cause: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled login left transport open")
	}
}

func TestTestConnectionContext_PropagatesCancellation(t *testing.T) {
	client := NewClient(&models.EmailAccount{IMAPHost: "127.0.0.1", IMAPPort: 993, Username: "u", Password: "p"}, NewConnectionPool())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.TestConnectionContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("connection test lost parent cancellation: %v", err)
	}
}

func TestAttachmentSemantics(t *testing.T) {
	for _, tt := range []struct {
		name string
		part goimap.BodyStructure
		want bool
	}{
		{name: "ordinary body", part: &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain"}},
		{name: "inline resource", part: &goimap.BodyStructureSinglePart{Type: "image", Subtype: "png", Extended: &goimap.BodyStructureSinglePartExt{Disposition: &goimap.BodyStructureDisposition{Value: "inline"}}}},
		{name: "attachment without filename", part: &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Extended: &goimap.BodyStructureSinglePartExt{Disposition: &goimap.BodyStructureDisposition{Value: "ATTACHMENT"}}}, want: true},
		{name: "inline filename", part: &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Extended: &goimap.BodyStructureSinglePartExt{Disposition: &goimap.BodyStructureDisposition{Value: "inline", Params: map[string]string{"filename": "notes.txt"}}}}, want: true},
		{name: "content type name", part: &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Params: map[string]string{"name": "notes.txt"}}, want: true},
		{name: "continued filename", part: &goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain", Extended: &goimap.BodyStructureSinglePartExt{Disposition: &goimap.BodyStructureDisposition{Value: "inline", Params: map[string]string{"filename*0*": "utf-8''notes"}}}}, want: true},
		{name: "multipart attachment", part: &goimap.BodyStructureMultiPart{Subtype: "mixed", Children: []goimap.BodyStructure{&goimap.BodyStructureSinglePart{Type: "text", Subtype: "plain"}}, Extended: &goimap.BodyStructureMultiPartExt{Disposition: &goimap.BodyStructureDisposition{Value: "attachment"}}}, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAttachment(tt.part); got != tt.want {
				t.Fatalf("hasAttachment=%v, want %v", got, tt.want)
			}
			if tt.want {
				sel := selectBodyParts(tt.part)
				if len(sel.textPath) != 0 || len(sel.htmlPath) != 0 {
					t.Fatalf("attachment selected as body: %+v", sel)
				}
			}
		})
	}
}

func TestFetchLatestEmails_FillsAttachmentMetadataWithoutBody(t *testing.T) {
	for _, tt := range []struct {
		name      string
		structure string
		want      bool
	}{
		{name: "plain body", structure: `("TEXT" "PLAIN" NIL NIL NIL "7BIT" 5 1 NIL NIL NIL NIL)`},
		{name: "inline resource", structure: `("IMAGE" "PNG" NIL NIL NIL "BASE64" 16 NIL ("INLINE" NIL) NIL NIL)`},
		{name: "attachment", structure: `("APPLICATION" "PDF" NIL NIL NIL "BASE64" 99999999 NIL ("ATTACHMENT" NIL) NIL NIL)`, want: true},
		{name: "inline file", structure: `("TEXT" "PLAIN" NIL NIL NIL "7BIT" 5 1 NIL ("INLINE" ("FILENAME" "notes.txt")) NIL NIL)`, want: true},
		{name: "content type filename", structure: `("IMAGE" "PNG" ("NAME" "photo.png") NIL NIL "BASE64" 16 NIL NIL NIL NIL)`, want: true},
		{name: "nested attachment", structure: `(("TEXT" "PLAIN" NIL NIL NIL "7BIT" 5 1 NIL NIL NIL NIL) (("IMAGE" "PNG" NIL NIL NIL "BASE64" 16 NIL ("INLINE" ("FILENAME" "photo.png")) NIL NIL) "RELATED" NIL NIL NIL NIL) "MIXED" NIL NIL NIL NIL)`, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := scriptedClient(t, func(tag, command string) string {
				if strings.HasPrefix(command, "EXAMINE ") {
					return mailboxResponse(tag, 3, 1)
				}
				if strings.HasPrefix(command, "FETCH ") && strings.Contains(command, "ENVELOPE") && strings.Contains(command, "BODYSTRUCTURE") && !strings.Contains(command, "FLAGS") && !strings.Contains(command, "BODY.PEEK") && !strings.Contains(command, "BODY[") {
					return "* 1 FETCH (UID 7 BODYSTRUCTURE " + tt.structure + ")\r\n" + tag + " OK summary\r\n"
				}
				t.Errorf("summary fetched unexpected data: %s", command)
				return tag + " BAD unexpected command\r\n"
			})
			emails, epoch, err := client.FetchLatestEmails(context.Background(), "INBOX", 50)
			if err != nil || epoch != 3 || len(emails) != 1 || emails[0].HasAttachment != tt.want {
				t.Fatalf("summary attachment metadata: emails=%+v epoch=%d err=%v, want attachment=%v", emails, epoch, err, tt.want)
			}
		})
	}
}

func TestFetchEmailBody_PropagatesFullMIMEFailures(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		fail bool
	}{
		{name: "valid empty body", raw: "Content-Type: text/plain\r\n\r\n"},
		{name: "missing message literal", fail: true},
		{name: "invalid MIME", raw: "broken header\r\n\r\n", fail: true},
		{name: "broken encoded body", raw: "Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n!!!!", fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := scriptedClient(t, func(tag, command string) string {
				if strings.HasPrefix(command, "EXAMINE ") {
					return mailboxResponse(tag, 1, 1)
				}
				if strings.Contains(command, "BODYSTRUCTURE") {
					return "* 1 FETCH (UID 7)\r\n" + tag + " OK structure\r\n"
				}
				if tt.raw == "" {
					return "* 1 FETCH (UID 7)\r\n" + tag + " OK body\r\n"
				}
				return fmt.Sprintf("* 1 FETCH (UID 7 BODY[]<0> {%d}\r\n%s)\r\n%s OK body\r\n", len(tt.raw), tt.raw, tag)
			})
			text, html, err := client.FetchEmailBody(context.Background(), "INBOX", 7, 1)
			if (err != nil) != tt.fail || text != "" || html != "" {
				t.Fatalf("text=%q html=%q err=%v; want failure=%v", text, html, err, tt.fail)
			}
		})
	}
}

func TestFetchEmailBody_MultipartDecodeFailureDoesNotReturnPartialBody(t *testing.T) {
	for _, tt := range []struct {
		name       string
		html       string
		omitHeader bool
		omitBody   bool
		fail       bool
	}{
		{name: "valid empty html"},
		{name: "valid html", html: "PGI+aGk8L2I+"},
		{name: "malformed html", html: "!!!!", fail: true},
		{name: "missing MIME section", omitHeader: true, fail: true},
		{name: "missing body section", omitBody: true, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := scriptedClient(t, func(tag, command string) string {
				if strings.HasPrefix(command, "EXAMINE ") {
					return mailboxResponse(tag, 1, 1)
				}
				if strings.Contains(command, "BODYSTRUCTURE") {
					structure := `(("TEXT" "PLAIN" NIL NIL NIL "7BIT" 5 1 NIL NIL NIL NIL) ("TEXT" "HTML" NIL NIL NIL "BASE64" 16 1 NIL NIL NIL NIL) ("APPLICATION" "PDF" NIL NIL NIL "BASE64" 99999999 NIL ("ATTACHMENT" NIL) NIL NIL) "MIXED" NIL NIL NIL NIL)`
					return "* 1 FETCH (UID 7 BODYSTRUCTURE " + structure + ")\r\n" + tag + " OK structure\r\n"
				}
				if !strings.Contains(command, "BODY.PEEK[1.MIME]") || !strings.Contains(command, "BODY.PEEK[1]") || !strings.Contains(command, "BODY.PEEK[2.MIME]") || !strings.Contains(command, "BODY.PEEK[2]") || strings.Contains(command, "[3") {
					t.Errorf("unexpected body sections: %s", command)
					return tag + " BAD unexpected sections\r\n"
				}
				literal := func(section, value string) string {
					return fmt.Sprintf(" BODY[%s]<0> {%d}\r\n%s", section, len(value), value)
				}
				response := "* 1 FETCH (UID 7" + literal("1.MIME", "Content-Type: text/plain\r\n\r\n") + literal("1", "hello")
				if !tt.omitHeader {
					response += literal("2.MIME", "Content-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\n")
				}
				if !tt.omitBody {
					response += literal("2", tt.html)
				}
				return response + ")\r\n" + tag + " OK body\r\n"
			})
			text, html, err := client.FetchEmailBody(context.Background(), "INBOX", 7, 1)
			if tt.fail {
				if err == nil || text != "" || html != "" {
					t.Fatalf("failed decode returned cacheable partial body: text=%q html=%q err=%v", text, html, err)
				}
				return
			}
			wantHTML := ""
			if tt.html != "" {
				wantHTML = "<b>hi</b>"
			}
			if err != nil || text != "hello" || html != wantHTML {
				t.Fatalf("text=%q html=%q err=%v, want html=%q", text, html, err, wantHTML)
			}
		})
	}
}

func TestIsAuthError_CodeLessTemporaryNO(t *testing.T) {
	err := &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Authentication failed temporarily; try again"}
	if IsAuthError(err) {
		t.Fatalf("temporary NO classified as invalid credentials: %v", err)
	}
}

func TestFetchBodySections_RejectsCumulativeOversize(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		first := strings.Repeat("a", maxBodyBytes/2)
		second := strings.Repeat("b", maxBodyBytes/2+1)
		return fmt.Sprintf("* 1 FETCH (UID 7 BODY[1]<0> {%d}\r\n%s BODY[2]<0> {%d}\r\n%s)\r\n%s OK body\r\n", len(first), first, len(second), second, tag)
	})
	_, err := client.fetchBodySections(goimap.UIDSetNum(7), []*goimap.FetchItemBodySection{{Part: []int{1}, Peek: true}, {Part: []int{2}, Peek: true}})
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("combined body sections exceeded limit without error: %v", err)
	}
}
