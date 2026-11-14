package application

import (
	"context"
	"testing"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

func TestSearchTreatsWildcardsAndQuotesAsLiteralText(t *testing.T) {
	db, pool := applicationTestDB(t)
	if err := database.InitializeSchema(db); err != nil {
		t.Fatal(err)
	}
	account := models.EmailAccount{}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	subjects := []string{"Discount 100% complete", "Discount 100x complete", "receipt_a", "receiptxa", `C:\mail\archive`, "C:mailarchive", "电子发票通知", `"quoted" report`}
	for _, subject := range subjects {
		if err := db.Create(&models.Email{AccountID: account.ID, Folder: "INBOX", Subject: subject}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, enabled := range []bool{false, true} {
		name := "LIKE"
		if enabled {
			name = "FTS5"
		}
		t.Run(name, func(t *testing.T) {
			if enabled && !database.IsFTS5Enabled() {
				t.Skip("FTS5 requires -tags fts5")
			}
			service := NewMailService(db, pool, enabled)
			for _, test := range []struct {
				search string
				want   string
			}{
				{"%", subjects[0]}, {"_", subjects[2]}, {`\`, subjects[4]},
				{"100%", subjects[0]}, {"t_a", subjects[2]}, {`\ma`, subjects[4]},
				{"电子发", subjects[6]}, {`"quoted"`, subjects[7]}, {`" OR "`, ""},
			} {
				result, err := service.List(context.Background(), ListEmails{Search: test.search})
				wantCount := 1
				if test.want == "" {
					wantCount = 0
				}
				if err != nil || len(result) != wantCount || (wantCount == 1 && result[0].Subject != test.want) {
					t.Fatalf("literal search %q: %+v %v", test.search, result, err)
				}
			}
		})
	}
}
