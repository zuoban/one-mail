package imap

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

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
