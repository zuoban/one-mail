package imap

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCachedRefreshFetchesMetadataOnlyForMissingUIDs(t *testing.T) {
	var metadata atomic.Int32
	client := scriptedClient(t, func(tag, command string) string {
		switch {
		case strings.HasPrefix(command, "EXAMINE "):
			return mailboxResponse(tag, 8, 2)
		case command == "FETCH 1:2 (UID)":
			return "* 1 FETCH (UID 2)\r\n* 2 FETCH (UID 3)\r\n" + tag + " OK UIDs\r\n"
		case strings.HasPrefix(command, "UID FETCH 3 ") && strings.Contains(command, "ENVELOPE") && strings.Contains(command, "BODYSTRUCTURE"):
			metadata.Add(1)
			return "* 2 FETCH (UID 3)\r\n" + tag + " OK summary\r\n"
		default:
			t.Errorf("unexpected incremental command: %s", command)
			return tag + " BAD unexpected command\r\n"
		}
	})
	cached := map[uint]EmailSummary{1: {UID: 1, Subject: "deleted"}, 2: {UID: 2, Subject: "reused", HasAttachment: true}}
	emails, epoch, err := client.FetchLatestEmailsCached(context.Background(), "INBOX", 50, 8, cached)
	if err != nil || epoch != 8 || len(emails) != 2 || metadata.Load() != 1 {
		t.Fatalf("incremental fetch failed: %+v epoch=%d metadata=%d error=%v", emails, epoch, metadata.Load(), err)
	}
	found := map[uint]*EmailSummary{}
	for _, email := range emails {
		found[email.UID] = email
	}
	if found[1] != nil || found[2] == nil || found[2].Subject != "reused" || !found[2].HasAttachment || found[3] == nil {
		t.Fatalf("UID reuse or remote deletion failed: %+v", found)
	}
}

func TestCachedRefreshUnchangedWindowAvoidsMetadataFetch(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		if strings.HasPrefix(command, "EXAMINE ") {
			return mailboxResponse(tag, 8, 1)
		}
		if command != "FETCH 1 (UID)" {
			t.Errorf("unchanged window fetched metadata: %s", command)
		}
		return "* 1 FETCH (UID 2)\r\n" + tag + " OK UIDs\r\n"
	})
	emails, _, err := client.FetchLatestEmailsCached(context.Background(), "INBOX", 50, 8, map[uint]EmailSummary{2: {UID: 2, Subject: "reused"}})
	if err != nil || len(emails) != 1 || emails[0].Subject != "reused" {
		t.Fatalf("unchanged cache lost: %+v %v", emails, err)
	}
}

func TestCachedRefreshEpochChangeFetchesFreshMetadata(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		if strings.HasPrefix(command, "EXAMINE ") {
			return mailboxResponse(tag, 9, 1)
		}
		if !strings.HasPrefix(command, "FETCH 1 ") || !strings.Contains(command, "ENVELOPE") {
			t.Errorf("new epoch did not fetch metadata: %s", command)
		}
		return "* 1 FETCH (UID 2)\r\n" + tag + " OK summary\r\n"
	})
	emails, epoch, err := client.FetchLatestEmailsCached(context.Background(), "INBOX", 50, 8, map[uint]EmailSummary{2: {UID: 2, Subject: "obsolete"}})
	if err != nil || epoch != 9 || len(emails) != 1 || emails[0].Subject == "obsolete" {
		t.Fatalf("old epoch was reused: %+v %d %v", emails, epoch, err)
	}
}

func TestCachedRefreshRejectsMalformedUIDWindow(t *testing.T) {
	for _, response := range []string{"* 1 FETCH (UID 0)\r\n", "* 1 FETCH (UID 2)\r\n* 2 FETCH (UID 2)\r\n"} {
		client := scriptedClient(t, func(tag, command string) string {
			if strings.HasPrefix(command, "EXAMINE ") {
				return mailboxResponse(tag, 8, 2)
			}
			return response + tag + " OK UIDs\r\n"
		})
		if _, _, err := client.FetchLatestEmailsCached(context.Background(), "INBOX", 50, 8, map[uint]EmailSummary{2: {UID: 2}}); err == nil {
			t.Fatal("malformed UID window was accepted")
		}
	}
}

func TestCachedRefreshRejectsMetadataOutsideUIDWindow(t *testing.T) {
	client := scriptedClient(t, func(tag, command string) string {
		if strings.HasPrefix(command, "EXAMINE ") {
			return mailboxResponse(tag, 8, 1)
		}
		if command == "FETCH 1 (UID)" {
			return "* 1 FETCH (UID 3)\r\n" + tag + " OK UIDs\r\n"
		}
		return "* 1 FETCH (UID 99)\r\n" + tag + " OK summary\r\n"
	})
	if _, _, err := client.FetchLatestEmailsCached(context.Background(), "INBOX", 50, 8, map[uint]EmailSummary{2: {UID: 2}}); err == nil {
		t.Fatal("unrequested metadata was accepted")
	}
}
