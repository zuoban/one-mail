package imap

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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

	decoded := decodeBodyPart(mimeHeader, []byte(encoded))
	if decoded != original {
		t.Fatalf("expected %q, got %q", original, decoded)
	}

	qpHeader := []byte("Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qpBody := []byte("<p>Hello=20World</p>")
	if got := decodeBodyPart(qpHeader, qpBody); got != "<p>Hello World</p>" {
		t.Fatalf("expected decoded quoted-printable, got %q", got)
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
	})

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

	if !IsAuthError(errors.New("imap: NO [AUTHENTICATIONFAILED] Login fail.")) {
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

func TestSetSeen_AddsRemovesFlagsAndChecksEpoch(t *testing.T) {
	for _, seen := range []bool{false, true} {
		t.Run(fmt.Sprint(seen), func(t *testing.T) {
			var stores atomic.Int32
			client := scriptedClient(t, func(tag, command string) string {
				if strings.HasPrefix(command, "SELECT") {
					return mailboxResponse(tag, 1, 1)
				}
				if strings.Contains(command, "FETCH") {
					return "* 1 FETCH (UID 7)\r\n" + tag + " OK uid\r\n"
				}
				if strings.Contains(command, "STORE") {
					stores.Add(1)
					op := "-FLAGS"
					if seen {
						op = "+FLAGS"
					}
					if !strings.Contains(command, op) {
						t.Errorf("wrong seen operation: %s", command)
					}
				}
				return tag + " OK stored\r\n"
			})
			if err := client.SetSeen(context.Background(), 7, "INBOX", 1, seen); err != nil {
				t.Fatal(err)
			}
			if stores.Load() != 1 {
				t.Fatal("seen flag was not written")
			}
			if err := client.SetSeen(context.Background(), 7, "INBOX", 2, seen); !errors.Is(err, ErrUIDValidityChanged) {
				t.Fatalf("epoch mismatch accepted: %v", err)
			}
			if stores.Load() != 1 {
				t.Fatal("epoch mismatch wrote a flag")
			}
		})
	}
}
