package imap

import (
	"encoding/base64"
	"reflect"
	"testing"

	goimap "github.com/emersion/go-imap/v2"
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

func TestSelectUIDBatch(t *testing.T) {
	uids := []goimap.UID{30, 10, 50, 20, 40}

	// Initial sync (lastUID == 0) grabs newest `limit` UIDs without pagination
	initialBatch, hasMore := selectUIDBatch(uids, 0, 3)
	if hasMore {
		t.Fatal("expected hasMore=false on initial sync")
	}
	if !reflect.DeepEqual(initialBatch, []goimap.UID{30, 40, 50}) {
		t.Fatalf("unexpected initial batch: %v", initialBatch)
	}

	// Incremental sync (lastUID > 0) grabs oldest `limit` UIDs sequentially and signals hasMore=true
	incBatch, hasMore := selectUIDBatch(uids, 5, 3)
	if !hasMore {
		t.Fatal("expected hasMore=true on incremental sync when len > limit")
	}
	if !reflect.DeepEqual(incBatch, []goimap.UID{10, 20, 30}) {
		t.Fatalf("unexpected incremental batch: %v", incBatch)
	}

	// Within limit returns all sorted UIDs with hasMore=false
	fullBatch, hasMore := selectUIDBatch(uids, 5, 10)
	if hasMore {
		t.Fatal("expected hasMore=false when len <= limit")
	}
	if !reflect.DeepEqual(fullBatch, []goimap.UID{10, 20, 30, 40, 50}) {
		t.Fatalf("unexpected full batch: %v", fullBatch)
	}
}
