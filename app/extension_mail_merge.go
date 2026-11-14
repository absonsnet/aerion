package app

import (
	"context"
	"fmt"

	extmailmergebe "github.com/hkdb/aerion/extensions/mail-merge/backend"
	"github.com/hkdb/aerion/internal/contact"
	hostmm "github.com/hkdb/aerion/internal/mailmerge"
	"github.com/hkdb/aerion/internal/smtp"
)

// initMailMergeExtension wires the Mail Merge extension's Bridge into App
// during Startup. All bridge logic lives in
// extensions/mail-merge/backend/; the merge engine, sources and run
// orchestration live in internal/mailmerge; this file exists ONLY so the
// host can supply the bridge with its host-provided dependencies (the
// host-constructed Service object, settings store, paths, coreapi handle)
// and so the embedded-field promotion makes the bridge methods
// Wails-bindable.
//
// Lightweight-by-default invariant: the Bridge struct allocation is the
// entire footprint until the first enabled `MailMerge_*` Wails call. At
// that point, `MailMergeBridge.ensureInit()` opens the per-extension
// SQLite and applies pending migrations. Disabled extensions contribute
// zero work.
//
// Per docs/EXT_RULES.md R2, this file holds NO closures wrapping
// `internal/*` calls. The host's touchpoints are plain adapter structs
// below (each wraps a host store/ops value and implements one narrow
// hostmm interface by calling its methods) plus the standard
// `coreapi.Core` surface for the toolbar button + settings tab.
func (a *App) initMailMergeExtension() {
	mailMergeCore := newCoreForExtension(a, a.mailMergeExt)

	a.MailMergeBridge = extmailmergebe.NewMailMergeBridge(extmailmergebe.MailMergeBridgeDeps{
		SettingsStore: a.settingsStore,
		Paths:         a.paths,
		Core:          mailMergeCore,
		Service:       newMailMergeService(&a.composeOps, &appMailMergeDraftSaver{a: a}, a.contactStore),
		Ctx:           a.ctx,
	})
}

// newMailMergeService builds the host-side merge service from the shared
// compose ops, a draft saver, and the contact store. Shared by App and
// ComposerApp (both carry composeOps + contactStore).
func newMailMergeService(ops *composeOps, draftSaver hostmm.DraftSaver, contacts *contact.Store) *hostmm.Service {
	return hostmm.NewService(hostmm.ServiceDeps{
		DraftSaver:    draftSaver,
		SenderFactory: &mailMergeSenderFactory{ops: ops},
		SentSaver:     &mailMergeSentSaver{ops: ops},
		ContactLister: &mailMergeContactLister{store: contacts},
	})
}

// appMailMergeDraftSaver adapts App.SaveDraft to hostmm.DraftSaver: one
// draft per row in "drafts" deliver mode. The draft ID/result is not
// needed by the run, so it is discarded.
type appMailMergeDraftSaver struct{ a *App }

func (s *appMailMergeDraftSaver) SaveDraft(accountID string, msg smtp.ComposeMessage) error {
	_, err := s.a.SaveDraft(accountID, msg, "")
	return err
}

// composerMailMergeDraftSaver adapts ComposerApp.SaveDraft to
// hostmm.DraftSaver (the detached composer's twin of the adapter above).
type composerMailMergeDraftSaver struct{ c *ComposerApp }

func (s *composerMailMergeDraftSaver) SaveDraft(accountID string, msg smtp.ComposeMessage) error {
	_, err := s.c.SaveDraft(accountID, msg, "")
	return err
}

// mailMergeSenderFactory builds the run's ONE reused SMTP connection:
// Connect + Login once, SendMail per row, Close at the end. The client
// config comes from composeOps.smtpClientConfig — the exact config a
// normal send uses (password or OAuth2, shared-mailbox / separate-SMTP
// username overrides, TLS).
type mailMergeSenderFactory struct{ ops *composeOps }

func (f *mailMergeSenderFactory) NewSender(ctx context.Context, accountID string) (hostmm.MailSender, error) {
	acc, err := f.ops.accountStore.Get(accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	if acc == nil {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}
	if acc.NoOutgoingServer {
		return nil, fmt.Errorf("account %q is configured as receive-only (no outgoing server)", acc.Email)
	}
	cfg, err := f.ops.smtpClientConfig(ctx, acc)
	if err != nil {
		return nil, err
	}
	client := smtp.NewClient(cfg)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	if err := client.Login(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to login to SMTP server: %w", err)
	}
	return &mailMergeSMTPSender{client: client}, nil
}

// mailMergeSMTPSender adapts *smtp.Client to hostmm.MailSender.
type mailMergeSMTPSender struct{ client *smtp.Client }

func (s *mailMergeSMTPSender) SendMail(from string, to []string, msg []byte) error {
	return s.client.SendMail(from, to, msg)
}

func (s *mailMergeSMTPSender) Close() error { return s.client.Close() }

// mailMergeSentSaver mirrors App.SendMessage's Sent handling: skip when
// the provider auto-saves Sent mail, else IMAP APPEND via
// composeOps.saveToSentFolder.
type mailMergeSentSaver struct{ ops *composeOps }

func (s *mailMergeSentSaver) SaveToSent(ctx context.Context, accountID string, rawMsg []byte) error {
	acc, err := s.ops.accountStore.Get(accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}
	if acc == nil {
		return fmt.Errorf("account not found: %s", accountID)
	}
	if providerAutoSavesSentMail(acc.IMAPHost) {
		return nil
	}
	return s.ops.saveToSentFolder(ctx, accountID, acc, rawMsg)
}

// mailMergeContactLister adapts contactStore.ListRecords (the unified
// local + CardDAV + Google + Microsoft set) to hostmm.ContactLister.
// It reads the injected contact store directly — coreapi.Contacts is nil
// in the detached composer process, so it must never be used here.
type mailMergeContactLister struct{ store *contact.Store }

func (l *mailMergeContactLister) ListContacts() ([]map[string]string, []string, error) {
	recs, err := l.store.ListRecords(contact.RecordFilter{})
	if err != nil {
		return nil, nil, err
	}
	rows := make([]map[string]string, 0, len(recs))
	for _, r := range recs {
		if r == nil {
			continue
		}
		rows = append(rows, hostmm.RecordToRow(r))
	}
	return rows, hostmm.ContactColumns(), nil
}
