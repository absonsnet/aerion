package app

import (
	"reflect"
	"testing"

	"github.com/absonsnet/daak/internal/account"
)

func TestCardDAVDiscoveryCandidates(t *testing.T) {
	cases := []struct {
		name string
		acc  *account.Account
		want []string
	}{
		{
			name: "GMX preset first, then generic domain, then IMAP host",
			acc:  &account.Account{Email: "me@gmx.net", Username: "me@gmx.net", IMAPHost: "imap.gmx.net"},
			want: []string{"https://carddav.gmx.net", "https://gmx.net", "https://imap.gmx.net"},
		},
		{
			name: "iCloud preset",
			acc:  &account.Account{Email: "me@icloud.com", Username: "me@icloud.com", IMAPHost: "imap.mail.me.com"},
			want: []string{"https://contacts.icloud.com", "https://icloud.com", "https://imap.mail.me.com"},
		},
		{
			name: "Posteo preset carries the port",
			acc:  &account.Account{Email: "me@posteo.de", Username: "me@posteo.de", IMAPHost: "posteo.de"},
			want: []string{"https://posteo.de:8843", "https://posteo.de"},
		},
		{
			name: "unknown provider: generic domain then IMAP host",
			acc:  &account.Account{Email: "me@example.org", Username: "me@example.org", IMAPHost: "mail.example.org"},
			want: []string{"https://example.org", "https://mail.example.org"},
		},
		{
			name: "IMAP host equal to email domain is deduplicated",
			acc:  &account.Account{Email: "me@example.org", Username: "me@example.org", IMAPHost: "example.org"},
			want: []string{"https://example.org"},
		},
		{
			name: "no IMAP host: domain only",
			acc:  &account.Account{Email: "me@example.org", Username: "me@example.org"},
			want: []string{"https://example.org"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cardDAVDiscoveryCandidates(c.acc); !reflect.DeepEqual(got, c.want) {
				t.Errorf("cardDAVDiscoveryCandidates = %v, want %v", got, c.want)
			}
		})
	}
}
