# Privacy Policy

**Last Updated: November 21, 2026**

Daak is an open-source desktop email client by ABSONS Technologies. The official Daak project page is https://github.com/absonsnet/daak

## Introduction

Daak is a desktop application that runs entirely on your computer. Daak does not operate any servers that collect, store, or process your data.

## Information We Do Not Collect

Daak does not collect any personal information on external servers. Daak does not transmit your emails, contacts, or credentials to us. Daak does not use analytics, telemetry, or tracking of any kind. Daak does not display advertisements. Daak does not sell or share your data — we do not have access to it.

## Information Stored Locally on Your Device

Daak stores the following information locally on your computer:

**Email data:** email messages synchronised from your email accounts, folder structure and organisation, attachments cached locally, and draft messages.

**Account credentials:** email account configurations (server addresses, ports), authentication tokens (OAuth tokens for Gmail and Outlook), and app-specific passwords. These are stored in your operating system's secure keyring, with an encrypted local store as a fallback if the keyring is unavailable.

**Contacts:** contact information synchronised from CardDAV, Google, or Microsoft accounts, used solely for email address autocomplete.

**Application settings:** your preferences and settings, window size and position, and theme preferences.

All of this data is stored in your local application data directory and never leaves your device except when communicating directly with your email provider, or when you use the optional AI Writing Assistant described below.

## Google API Services

Daak uses Google APIs to provide Gmail integration. Our use of information received from Google APIs adheres to the Google API Services User Data Policy, including the Limited Use requirements: https://developers.google.com/terms/api-services-user-data-policy

**Google API scopes used:**
- https://mail.google.com/ — to read, send, and manage your Gmail
- https://www.googleapis.com/auth/calendar — to view and manage your calendar
- https://www.googleapis.com/auth/contacts.readonly — to read your contacts
- https://www.googleapis.com/auth/contacts.other.readonly — to read your other contacts
- https://www.googleapis.com/auth/userinfo.email — to identify your account email address
- openid — to sign you in

**Limited Use disclosure:** Daak's use and transfer of information received from Google APIs to any other app will adhere to the Google API Services User Data Policy, including the Limited Use requirements. Specifically: Daak only uses Google data to provide email, calendar, and contact functionality to you. Daak does not use Google data for advertising. Daak does not use Google data to train AI/ML models. Human access to Google data is limited to debugging with your consent.

The one exception is the optional AI Writing Assistant below: if you use it on a message, the draft text you are working on is sent to the AI provider you chose. That is the only case in which Google data is sent to a third party, and it happens only when you use that feature.

## Microsoft API Services

Daak uses Microsoft Graph APIs to provide Outlook / Microsoft 365 integration.

**Microsoft API scopes used:**
- Mail.ReadWrite — to read, send, and manage your emails
- Calendars.ReadWrite — to view and manage your calendar
- Contacts.Read — to access your contacts for email autocomplete
- User.Read — to access your basic profile information
- offline_access — to maintain your session

All Microsoft data is stored locally and used solely for email, calendar, and contact functionality.

## AI Writing Assistant (optional)

The AI Writing Assistant is optional and off unless you set it up with your own API key. When you use it, the draft you are working on — and, if enabled, up to 3,000 characters of quoted message history — is sent to the AI provider you chose: OpenAI, Groq, Gemini, xAI, or Ollama on your own computer. It is sent only when you use the feature, and only to that provider. Your API key is stored locally on your device. That provider's own privacy policy applies to the text you send it. If you choose Ollama on your own computer, the text stays on your device.

## Third-Party Services

Daak only connects to services you explicitly configure: your email provider (via IMAP/SMTP), Google APIs (if you use Gmail), Microsoft APIs (if you use Outlook / Microsoft 365), CardDAV servers (if you configure contact sync), and your chosen AI provider (only if you use the AI Writing Assistant). We do not integrate with any advertising networks, analytics services, or other third-party data collectors.

## Data Security

Your data security depends on: (1) your device security — Daak stores data locally, so your device's security measures (disk encryption, password protection) protect your email data; (2) secure connections — all connections to email servers use TLS/SSL encryption; (3) system keyring — sensitive credentials are stored in your operating system's secure credential storage; (4) no cloud exposure — your data is never transmitted to our servers because we don't operate any.

## Data Retention and Deletion

Since all data is stored locally on your device, you control retention — data remains until you delete it. Uninstalling Daak removes the application but may leave data in your local application data directory. To remove it completely, delete the Daak data directory.

## Children's Privacy

Daak is not directed at children under 13. We do not knowingly collect information from children. Since Daak stores all data locally and does not transmit data to external servers, parental supervision of the device itself is the appropriate safeguard.

## Changes to This Privacy Policy

We may update this Privacy Policy from time to time. Changes will be reflected in the "Last Updated" date. Since Daak is open source, you can review any changes in our public repository.

## Open Source Transparency

Daak is open-source software. You can review exactly how we handle your data by examining our source code at https://github.com/absonsnet/daak

## Contact Us

If you have questions about this Privacy Policy, please contact us at absons.net@gmail.com, or open an issue at https://github.com/absonsnet/daak/issues
