// Package mail is the SMTP transport for transactional e-mail (M10 EM-2; design:
// docs/plan/m10-platform.md §4, ADR 0022). Standard library only, and it knows
// nothing of invitations or resets: a Message is an address, a subject, two
// bodies and an optional correlation label. It has no queue and no logger — it
// returns a Receipt or a *SendError and the caller logs the class and the code.
//
// Flow of one Send: validate and compose (every refusal here, before any dial) →
// dial with the deadline → greeting, EHLO → STARTTLS, REQUIRED (TLS ≥ 1.2, the
// certificate verified against Host) → AUTH PLAIN → MAIL, RCPT → DATA by hand →
// QUIT without waiting for its reply. Every byte read from the relay in one
// attempt is counted against a cap. One 4xx is retried once after a pause on a
// new connection, inside the same deadline. Why net/smtp's SendMail and
// Client.Data are not used: SMTP.attempt's comment.
//
// THE CLAIM, IN THREE PARTS AND ONLY THREE (agent-brief, M10 OP-6/OP-7; the
// canonical form is internal/db/logparams.go at boundParameterModes).
//
// PART I — TODAY'S SHIPPED CODE, MEASURED against an in-process SMTP server on
// 127.0.0.1 with certificates generated per run (fakesmtp_test.go):
//   - no TLS, no AUTH: STARTTLS not advertised, refused (454, 502), an untrusted
//     certificate, a trusted certificate for another name or without the dialled
//     name, or a server capped at TLS 1.1 each end in tls_unavailable with zero
//     AUTH commands at the server and no MAIL, with Host "127.0.0.1" and with
//     "localhost" — TestSend_NoTLSMeansNoAuth (its control rows show the counter
//     counts and a certificate for exactly the dialled name connects). Those are
//     the names for which net/smtp's PlainAuth itself sends credentials without
//     TLS — TestPlainAuth_SendsInClearForTheLoopbackNames — so the zero is this
//     package's STARTTLS gate, not PlainAuth's guard;
//   - the reply cap: a relay sending 16 times maxReplyBytes — one line without a
//     line end, continuation lines, before TLS or through it — ends the attempt as
//     network within the 2 s the test allows (against a 5 s deadline) and the
//     send allocates at most 16 times the cap — TestSend_CapsWhatTheRelayCanMakeItRead;
//     the one counter spans STARTTLS (200 KiB before TLS + 100 KiB after it is
//     network, either half alone is not) and the TLS handshake (a cap reached
//     inside it is network, not tls_unavailable) — TestSend_TheReplyCapCoversTheWholeAttempt.
//     ONE EXCEPTION, decided: when the line the cap cuts is the 250 reply to the
//     end of data, its code was read, so the send is ACCEPTED (MessageID empty:
//     the cut line is longer than an id may be) — TestSend_ACutReplyToTheEndOfDataIsAcceptance;
//   - header values: the rows of TestSend_RefusesBadInputBeforeAnyDial are each
//     refused with their class while the server accepts zero connections, and its
//     controls (plus-addressing, upper case, a domain literal, a 254-byte address)
//     are accepted and reach RCPT verbatim; on generated inputs compose refuses
//     exactly what an independent oracle refuses, and what it accepts has exactly
//     the expected header lines, To verbatim, a Subject decoding to the input and
//     7-bit lines of at most 998 octets — FuzzCompose; the final header gate alone
//     — TestHeaderBlock_RefusesWhatItCannotWriteVerbatim;
//   - the message the server receives: the expected headers once each, a
//     non-ASCII subject RFC 2047-encoded and an ASCII one unchanged, two
//     quoted-printable parts decoding to the inputs, a bare Reply-To, and
//     Message.Ref in no command line and no message line —
//     TestSend_TheMessageOnTheWire; a lone "." line is sent as ".." and the
//     conversation stays in step — TestSend_DotStuffsALoneDotLine;
//   - the conversation and the credentials: EHLO, STARTTLS, handshake, EHLO,
//     AUTH (over TLS, carrying the configured values), MAIL, RCPT, DATA, QUIT —
//     TestSend_DeliversOverSTARTTLSAndReturnsTheProvidersMessageID;
//   - the receipt: the id comes from the 250 reply; the forms listed in
//     TestSend_ReadsTheMessageIDFromThe250Reply yield "" and its realistic SES id
//     is kept — TestMessageID_EnhancedStatusIsOnlyStripped for the status prefix;
//   - time: a server that stops answering at any step the test lists (the TLS
//     handshake included) releases Send within 100 ms of a context deadline, of
//     Config.Timeout and of a cancellation — TestSend_ReturnsWithin100msOfTheDeadline;
//     a server that never answers QUIT does not hold a delivered send for more
//     than the 1 s the test allows — TestSend_AServerThatNeverAnswersQUITDoesNotHoldTheSend;
//   - retry: one 4xx retried on a new connection, a second is final (exactly two
//     connections), no retry without time for the pause or after a cancellation
//     — TestSend_RetriesA4xxOnceOnANewConnection; every row of
//     TestSend_ClassifiesEachStep asserts its class, its code AND its connection
//     count: two for each 4xx row, exactly one for every other — a 5xx at the
//     greeting, AUTH, MAIL, RCPT, DATA or the end of data, tls_unavailable, an
//     unexpected code, a broken stream; a connection dropped after the
//     terminating dot or reset during DATA is network after one connection —
//     TestSend_ANetworkFailureIsNotRetried;
//   - no server text: refusals naming the recipient and carrying a link render
//     with neither (nor the relay's prose) in the forms TestSendError_CarriesNoServerText
//     lists, and the *textproto.Error is unreachable (with a control showing bare
//     net/smtp's error does carry both);
//   - credentials: in each rendering TestCredential_IsRedactedInEveryRendering
//     lists, neither the password nor the username nor the first 8 characters of
//     either appears (inside Config and SMTP the fields print as a pointer
//     address); a Message prints as its placeholder in the renderings
//     TestMessage_PrintsAsAPlaceholder lists;
//   - configuration: New refuses each invalid field and quotes no value, and holds
//     Reply-To to the recipient rule — TestNew_RefusesAnInvalidConfig; only New
//     builds a usable sender — TestSend_ANilOrZeroSMTPIsNotConfigured;
//   - the recipient rule, exported for a caller that STORES an address for a later
//     send (M10 EM-6): ValidRecipient answers false exactly when Send refuses the
//     value as invalid_address, on every value TestValidRecipient_AgreesWithSend
//     lists, and a refused value is never dialled.
//
// PART II — NAMED PINS, AND EXACTLY WHAT EACH CATCHES:
//   - TestSendError_HasOnlyAClassAndACode: SendError's fields are exactly
//     Class (a string kind) and SMTPCode (int), *SendError's method set is
//     exactly {Error} and SendError's is empty — a new field, an Unwrap/Is/As or
//     a Format method turns it red;
//   - TestMail_ImportsOnlyTheStandardLibrary: an import whose first path element
//     contains a dot, in a non-test file of this directory;
//   - the compile-time assertions in credential.go and mail.go: a redaction
//     method removed from Credential or Message;
//   - the measured limits: TestCredential_KnownLimitIsReflection (reflection
//     reads a Credential) and TestMessage_KnownLimitIsAnUnexportedField (fmt
//     prints a Message held in an unexported field) — each turns red when its
//     limit stops being true, so the text stating it is updated.
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
//
// KNOWN LIMITS (measured where a test is named): the raw connection's
// SetDeadline is not separately pinned (the AfterFunc reaches the same deadline
// — SMTP.attempt); the explicit TLS MinVersion equals Go's current client
// default, so deleting it changes nothing a test can see; classify checks the
// reply cap before the time, and no test separates the two (a cap error and an
// ended context at the same instant is a race no test can schedule); the echo
// rule does not catch a TRANSFORMED echo (base64, case), an echo cut by a
// separator (".", "_" or "-") at least every 7 characters with its order kept,
// a REORDERED echo, or an embedded fragment shorter than echoWindow; a Subject
// line holding RFC 2047 encoded words is not folded to
// RFC 2047's 76 characters, only held under 998 (the subjects are ASCII);
// a Message in an unexported field, and a caller logging its fields, print the
// link and the address (TestMessage_KnownLimitIsAnUnexportedField); reflection
// reads a Credential (TestCredential_KnownLimitIsReflection); behaviour against a
// real relay (SES) is EM-5's to measure.
package mail
