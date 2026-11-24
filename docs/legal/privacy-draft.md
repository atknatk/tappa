# Gizlilik metni — TASLAK (backlog T117)

> 🔴 **TASLAKTIR. HUKUKİ GÖRÜŞ DEĞİLDİR. YAYINDAN ÖNCE BİR AVUKAT OKUMALIDIR.**
> Bu dosyayı bir ajan, yalnız bu depodaki olgulardan (kod, migration, deploy belgeleri, ADR'ler,
> kullanıcı kararları) derledi. Doğrulanamayan her cümle metnin içinde `[DOĞRULA: …]` ile
> işaretli; **içinde tek bir `[DOĞRULA` kalmış metin yayımlanmaz.** GDPR madde 13/14'ün istediği
> her başlığın burada doğru doldurulduğunu bu dosya iddia etmez.

**Ne için:** `legal_documents`'in `privacy` belgesi — canlıda hiç yayımlanmadı (T117, prod ölçümü
2026-10-21). Yayın kullanıcının işidir: `/operator/legal` → *Privacy* → metni yapıştır → yayımla.

**İçinde olması kararlaştırılmış iki satır (kullanıcı kararları):**

1. Cloudflare R2 (AB yargı bölgesi) — istemci tarafında şifreli veritabanı yedekleri için alt işleyici
   (T45, 2026-10-09).
2. Uygulama log'ları 15 gün saklanır (Q28 (b), kullanıcı 2026-10-14). ⚠️ Bu cevabın kaydı
   (`2ef582ea`, dal `m10-a1`) bu dalda ve `main`'de **yok**; burada `docs/plan/open-questions.md`
   Q28 hâlâ *"(b) açık"* diyor — backlog T285.

## Biçim — `/operator/legal` neyi bekler (okundu)

- **Düz metin, Markdown DEĞİL** (`internal/domain/legal/legal.go` `Paragraphs`; ADR 0033). Boş satır
  yeni paragraf; paragraf içindeki tek satır sonu bir **boşluk** olur. `**kalın**`, `#` başlık ve liste
  işaretleri olduğu gibi harf olarak görünür — bu yüzden aşağıda başlıklar kısa paragraflar, maddeler
  ayrı paragraflardır.
- Bağlantı dilbilgisi kapalıdır: `[etiket](https://…)`, çıplak `https://` adresi, çıplak e-posta adresi
  (→ `mailto:`) ve `+` ile başlayan uluslararası telefon (→ `tel:`). Başka hiçbir şey yorumlanmaz.
- Üst sınır 256 KiB (`internal/handler/operator/legal.go` `maxLegalBody`); boş gövde reddedilir.
- Tablo append-only'dir: yayımlanan her sürüm kalır; düzeltme yeni sürümdür.
- Sayfa başlığını (*"Privacy policy"*) ve yayın tarihini sayfa kendisi basar; metin onu tekrarlamaz.

**Yapıştırılacak metin aşağıdaki kod bloğunun içidir** (blok çizgileri hariç). İlk paragraf taslak
uyarısıdır ve yayından önce silinir.

## Yapıştırılacak metin

```text
[TASLAK — YAYINDAN ÖNCE BU PARAGRAFI VE BÜTÜN DOĞRULA İŞARETLERİNİ KALDIR. Bu metin hukuki görüş değildir; bir avukat onaylamadan yayımlanmaz.]

This notice explains what Taptime records about the people who use it, why, where it is kept, for how long, and how to exercise your rights over it.

1. Who is responsible

Taptime is a time and attendance service for businesses. A business (the "employer") subscribes, mounts Taptime plaques at its venues, and invites its staff, who then clock in and out by touching the plaque with their own phone.

For the attendance records of a business's staff, the employer decides why and how they are kept. The employer is the data controller for those records, and Taptime processes them on the employer's behalf, under the employer's instructions. [DOĞRULA: denetleyici/işleyen ayrımı ve müşteriyle veri işleme sözleşmesi (DPA) — Q23 açık; böyle bir sözleşme bugün yok.]

For the accounts of the people who run a business's dashboard, for billing, and for this website, Taptime is the data controller.

Taptime is operated by [DOĞRULA: tüzel kişinin tam unvanı, hukuki biçimi, sicil numarası ve kayıtlı adresi — yayımlanmış "Company details" metniyle (legal_documents imprint) birebir aynı olmalı; sitenin alt bilgisindeki imza "Codevera Labs" (web/templates/layout/credit.go)]. Contact for data protection requests: [DOĞRULA: veri koruma iletişim adresi; atanmışsa veri koruma görevlisi].

2. What we record about staff who clock in

The employer enters, and Taptime stores: your full name; your venue and, if the employer uses them, your department and job title; your e-mail address, if the employer enters one to send your invitation; the employer's own reference or code for you, if it connects Taptime to another system; and the dates your invitation was sent, your activation finished and, if it happens, your account was switched off.

When you activate Taptime on your phone, the browser keeps a sign-in cookie. In its database, Taptime stores only a hash of it, never the cookie itself, together with when it was created and last used and a short device label such as "iPhone Safari". The label is chosen from a fixed list; Taptime's database does not store your phone's full browser identification. [DOĞRULA: ingress erişim log'u — ingress-nginx'in varsayılan satırı IP, User-Agent ve sorgu dizesini taşır; bu satırlar uygulama log'larıyla aynı yerde tutuluyorsa bölüm 9'daki log cümlesi onları da kapsar.]

Each time you touch a plaque, or scan the QR code printed on it, Taptime records: the time; which plaque and venue; whether it was a clock-in or a clock-out; the result of the checks below; the network (IP) address your phone connected from; and, only if your phone shares it at that moment, your position as latitude and longitude. It also records the shift that applied and the outcome (approved, flagged for a manager, refused or ignored as a duplicate), with a short note explaining it.

If you ask your manager to correct a record, or a manager enters or reviews a record for you, that request or decision is recorded too, with who made it. Records are never edited or deleted after the fact: a correction is a new record next to the original, and the change is logged.

3. What we do not collect

Taptime collects no biometric data: no fingerprints, no face scans, no voice. Your phone's own screen lock stays on your phone; Taptime receives nothing from it.

Taptime does not track your location. Your phone's position is read only at the moment you press the button after touching a plaque, never in the background, never between taps and never after you leave the page.

Taptime sets no advertising or analytics cookie and embeds nothing from another website. Every cookie it sets is listed, with its purpose and lifetime, at https://taptime.mt/legal/cookies

4. Why the checks exist

A clock-in is checked before it is accepted: the plaque's chip proves the phone touched it at that moment; the sign-in cookie says who you are; the network address shows whether you are on the venue's own network; and the position, if shared, is compared with the venue's location (within 150 metres). A record that cannot be confirmed is still kept and goes to a manager for review, so that a working hour is never silently lost.

5. What we record about dashboard users

For the owner, managers and other people who use a business's dashboard, Taptime stores: name; e-mail address; password, only as a one-way hash (bcrypt), never the password itself; when the account was created and last signed in; sign-in sessions, again only as hashes; and a log of the changes made in the dashboard (who did what, when).

When a business signs up, Taptime also stores the business's name, VAT number, type of business and venues, and sends the VAT number to the European Commission's VAT register (VIES) to check it. A sign-up waiting for e-mail confirmation is valid for at most 24 hours.

6. Why we use the data, and on what basis

To run the service the employer subscribed to: recording working time, showing it to the employer's managers, and producing the employer's reports and exports. [DOĞRULA: her kategori için hukuki dayanak (GDPR md. 6) — işveren için muhtemelen çalışma süresinin kaydı yükümlülüğü ve/veya meşru menfaat; Taptime'ın kendi hesapları için sözleşmenin ifası. Avukat yazmalı.]

To keep the service secure and working: detecting refused plaques, failed checks and attempts on switched-off accounts, and investigating faults. [DOĞRULA: meşru menfaat — denge testi.]

To bill the business: a month is charged by the number of people who were active at any point in it.

Taptime does not sell personal data and does not use it for advertising or profiling.

7. Who receives the data

The employer's dashboard users see their own business's records. Except as described below, no other business can see them: every record belongs to one business, and the database itself refuses to show it to another.

If the employer belongs to a group of businesses in Taptime and you clock in at a venue of another business in that group, that business sees, by name, that you clocked in at its venue on behalf of your employer, [DOĞRULA: tam olarak hangi alanlar — site_punches çalışanın adını, işverenin referansını/kodunu, zamanı ve yönü taşır (migration 00046; ADR 0027)].

If the employer has Taptime's integration interface switched on, your attendance records are also sent to the employer's own systems at the employer's request. Taptime switches this on per business.

Through the product, Taptime's own staff can reach a business's data only through a separate operator console, and every such access is written to an audit log.

People who administer the server and the database have technical access to all data stored there, including the database and the backups. [DOĞRULA: kim (kişi/şirket), hangi durumda ve nasıl kayıt altına alınıyor — deploy kimliğinin tappa-postgres-0 üzerinde pods/exec yetkisi var (deploy/k8s/01-rbac.yaml) ve runbook kubectl … psql kullanıyor (deploy/README.md); bu erişim operator_audit_log'a düşmez.]

Taptime uses the following service providers (sub-processors). Each processes data only to provide its service to Taptime:

- Hetzner — hosting of the application, its database and its log collector, in a data centre in Falkenstein, Germany. [DOĞRULA: sözleşme tarafı tüzel kişi (Hetzner Online GmbH?) ve DPA; Hetzner hesabının sahibi kim — Taptime'ın kendisi mi, sunucuyu işleten başka bir taraf mı.]

- Amazon Web Services (Amazon SES), region eu-central-1 (Frankfurt) — sending invitation, sign-up confirmation, password reset, password-changed and business-link e-mails; it receives the recipient's e-mail address and the message. [DOĞRULA: sözleşme tarafı (AWS EMEA SARL?), DPA, ABD'li ana şirket nedeniyle aktarım konumu.]

- Cloudflare R2, EU jurisdiction — storage of database backups, which are encrypted on Taptime's own server before they are sent, so Cloudflare receives only encrypted data. [DOĞRULA: sözleşme tarafı tüzel kişi, kabul edilen self-serve DPA, ABD'li ana şirket nedeniyle aktarım konumu.]

- [DOĞRULA: sunucuyu, kümeyi ve log toplayıcıyı (SigNoz) işleten taraf — deploy/README.md kümeyi işleteni "Tappa değil" diye anıyor. Bu ayrı bir tüzel/gerçek kişiyse veritabanına, log'lara ve yedek şifreleme anahtarına erişimi olan bir alt işleyicidir ve burada adıyla yazılmalı; Taptime'ın kendisiyse bu madde silinir.]

- [DOĞRULA: healthchecks.io — çalışma/uyarı bildirimi; okunan uyarı kuralları (deploy/observability/signoz-alert-rules.json) yalnız toplam sayı gönderiyor, kişisel veri değil. Kişisel veri almadığı teyit edilirse bu madde silinir.]

The business's VAT number is checked against the European Commission's VIES register at sign-up and on request; this is a check against a public register, not a processing service.

8. Where the data is kept

The application, the database and the logs run on one server in Falkenstein, Germany [DOĞRULA: sunucuyu ve kümeyi işleten taraf — bölüm 7]. E-mail is sent from the EU (Frankfurt). Backups are stored in Cloudflare's EU jurisdiction. [DOĞRULA: AB dışına aktarım — AWS ve Cloudflare'in ABD'li ana şirketleri; standart sözleşme maddeleri / AB-ABD Veri Gizliliği Çerçevesi konumu.]

9. How long we keep it

Attendance records: [DOĞRULA: saklama süresi KARARA BAĞLI DEĞİL (Q13). Yapılandırmadaki TAPPA_RETENTION_YEARS="2" bir geliştirme yer tutucusudur, avukattan geçmedi ve hiçbir kod onunla kayıt silmiyor (deploy/k8s/05-config.yaml). Bugün kayıtlar süresiz tutuluyor; silme/anonimleştirme akışı yok. Yayımlanan süre ürünün gerçekten uyguladığı süre olmalı — önce Q13 kapanmalı.]

Application logs: logs record requests to the service, which may include network addresses, the browser identification and internal record identifiers. The log collector keeps them for 15 days and then deletes them. A copy also stays on the server next to the running service until the next software release replaces it, and that copy is limited in size rather than in time. [DOĞRULA: konteyner kopyası boyutla (10 MiB × 5) ve bir sonraki deploy'la sınırlı; deploy seyrekse 15 günü aşar (deploy/README.md; m10-a1 dalındaki Q28 (b) notu). Bu cümle bunu söylüyor; metin düz "15 gün" diyecekse kopya da 15 günle sınırlanmalı.]

Backups: 30 days, then deleted. A backup contains the whole database, so a record removed from the service survives in backups until they expire. [DOĞRULA: BACKUP_RETENTION_DAYS=30 (deploy/k8s/50-backup.yaml) "sayılmış bir sınırdır, hukuki görüş değil"; R2 kova kilidi 14 gün.]

Sign-in sessions: until they expire or are revoked; see the cookie list for their lifetimes. [DOĞRULA: süresi dolan oturum satırlarının veritabanında ne kadar kaldığı — silme yok.]

10. Your rights

You can ask for a copy of your data, its correction, its erasure, a restriction of its use, or to object to it, and you can ask for the data you provided in a portable format. [DOĞRULA: haklar listesi ve sınırları — özellikle silme: mesai kayıtları değişmezdir (yalnız düzeltme kaydı eklenir); silme talebinin nasıl karşılanacağı Q13'te açık (öneri: çalışan anonimleştirilir, kayıtlar korunur — hukuki onay ister).]

If you clock in for an employer, send your request to your employer first: the employer decides about its staff records, and Taptime will help it answer. You can also contact Taptime directly at [DOĞRULA: iletişim adresi].

You have the right to complain to a data protection supervisory authority. [DOĞRULA: yetkili denetim makamı — Malta'daki Information and Data Protection Commissioner mı; adres.]

11. Security

Records are stored in a database where each business's data is separated at the database level. Sign-in cookies and passwords are stored only as hashes. Backups are encrypted before they leave the server. The secret keys of the plaques are stored encrypted and are never written to logs.

12. The Taptime staff app

[TASLAK — BÖLÜM 12 HUKUKİ İNCELEME İÇİNDİR (M16-41). Her cümle 6fdeaced ağacındaki koda karşı okundu; kaynaklar aşağıda "Bölüm 12'nin kaynağı" tablosunda. Kodda olmayan iki iddia "[Only if … ships: …]" köşeli parantezindedir (e-postayla giriş — ADR 0049, M16-35; cihaz doğrulaması — Faz E): özellik yayından önce canlıya çıkmadıysa parantez metniyle birlikte silinir, çıktıysa yalnız parantez işaretleri kalkar. Kilit ekranı sayacı ve ana ekran widget'ı iki platformda da var (iOS: Live Activity + WidgetKit, 250bd852); sahiplere e-posta 13f24033 ile geldi. Bu paragraf yayından önce silinir.]

Taptime also offers an app for staff, for iPhone and Android. Using it is optional: clocking in with the phone's browser works as before. This section describes what is different when you use the app. Everything else in this notice applies to the app as well.

Activating the app. You type or paste the invitation code your employer sent you. Before you go on, the app shows who your employer is and asks for your consent. The activation is finished by your first touch of one of your employer's plaques; that touch is not recorded as a clock-in. Activating the app signs you out of Taptime on your other phones and browsers for that employer.

What Taptime stores about your phone. During activation your phone creates a signing key inside its own secure hardware (the Secure Enclave on iPhone, the Android Keystore on Android). The private half of the key never leaves the phone and cannot be copied from it, not even into a backup. Taptime stores only the public half, which lets it check that each request really comes from your phone; the app uses no password and no sign-in cookie. Next to the public key, Taptime stores which platform the phone runs (iPhone or Android) and a coarse label, "iPhone" or "Android", which Taptime sets itself: the app sends no phone model, phone name or other identifier for it. Taptime also stores when the phone was activated, when it was last used and, if it happens, when it was signed out. As in the browser, each request carries your phone's network (IP) address, and a clock-in or clock-out stores it with the record (section 2). [DOĞRULA: uygulama platformun varsayılan HTTP User-Agent'ını gönderir (Android: işletim sistemi sürümü ve telefon modeli; iOS: uygulama adı ve CFNetwork/Darwin sürümü). Taptime uygulaması onu saklamaz ve log'a yazmaz; ingress erişim log'u yazıyorsa bölüm 2'deki ingress notu ve bölüm 9'daki log cümlesi bunu da kapsamalı.]

What the app keeps on your phone: the key, inside the secure hardware; the identifier Taptime gave the phone; your name as your employer entered it; your choices for the reminder and the timer described below; and, each time the app asks Taptime whether you are clocked in, the answer as a few times (when you clocked in, and until when to show it), which the reminder, the timer and the widget read. These times are kept even if you use none of the three, and they are deleted when the phone is removed or signed out. On Android, the app's data is excluded from backups and from transfers to a new phone.

Your location. The app asks for location access only when you clock in or out, never when it starts, and only while you are using it. When you press the button after touching a plaque, the app takes one reading of your position and sends it with that clock-in or clock-out. Taptime stores that one position with that record, as described in section 2, and nothing else: the app never reads your location in the background, between taps or after the tap, and Taptime keeps no history or track of your positions. If you do not allow location access, you can still clock in; the record is checked with the other evidence and, if it cannot be confirmed, goes to a manager (section 4).

Simulated locations. Your phone's operating system says whether a position came from software that simulates a location. The app passes this on only when the phone says the reading was simulated, and never without the reading itself. Taptime keeps that report with the record's check results. By default, such a clock-in is still recorded and is sent to a manager for approval instead of being accepted; your employer can change this rule.

[Only if device attestation ships: The app may also ask Apple (App Attest) or Google (Play Integrity) to confirm that it is the genuine Taptime app on a genuine phone, and Taptime keeps the result with the phone's record. — Not in this version: the iOS app has no App Attest entitlement and the Android app has no Play Integrity library (ADR 0045, phase E). If it ships, Apple's and Google's role must also be checked for section 7.]

[Only if e-mail sign-in ships: If your employer turns it on, you can ask for a new sign-in link by entering your work e-mail address, the one your employer entered for you. Taptime uses the address only to find your record and to send the link to it. The app does not keep the address on your phone, Taptime does not write it to its logs, and the answer is the same whether or not the address is known. Signing in is still finished by a touch of your employer's plaque, and your employer can see it. — Not in this version of the app or the server (ADR 0049, card M16-35).]

My hours. If your employer turns it on (it is off until they do), the app's My hours screen shows your own records and weekly totals, for the period your employer chooses: 1, 2, 4 or 13 weeks, counting the current week. For each record it shows the date and time, the venue, whether it was a clock-in or a clock-out, how it was made, whether it is approved, waiting for a manager, refused or a repeat, and the state of any request you sent about it. It does not show your position, your network address or the score of the checks. The screen reads your records from Taptime when you open it; they are not kept on your phone, and opening the screen changes nothing in them. If your employer marks its shift times as its staff's real shifts, the app also shows your usual shift. The same records and totals, under the same rules, can also be opened in the browser you clock in with (the browser's My hours page); it shows nothing to a browser that is not signed in as you, and nothing is kept in the browser's cache.

Missing times. When one of your records lacks its other half (for example, you clocked in but there is no clock-out), the app can offer to send your employer the missing time. You give the time; Taptime stores your request, the time you gave and when you sent it, as part of your employer's records. A request is not a clock-in record and adds no hours. Hours are added only if a manager approves the request, and then as a new record marked as entered by that manager; if the manager rejects it, nothing is added. Requests, like records, are never edited or deleted afterwards. The app shows you the requests you sent and what happened to them.

Reminders, lock-screen timer and home-screen widget. The app can remind you to clock out ("Still at work?") if you may have forgotten. It can also show a running timer on the lock screen while you are clocked in (on iPhone, as a Live Activity), and you can add a Taptime widget to your home screen. The reminder and the timer are off until you turn them on, and the widget appears only if you add it. All three are set up on your phone, by your phone: Taptime uses no push-notification service for them, receives no push token, and collects nothing new for them. The phone works them out from the clock-in time the app already receives when you open it, and none of them uses your location. They show times only: no name, no business and no venue (the timer adds your shift's end time only if your employer marks its shifts as real). What the widget and the timer keep on the phone is only the time you clocked in, the time until which to show it as current (no later than the end of that business day) and, where your employer marks its shifts as real, your shift's end time. Your on/off choices stay on the phone.

Removing a phone. In the app's settings, "Remove this phone" signs this phone out of Taptime and deletes its key from it. Your other phones and your records are not affected.

Deleting your account. The app's settings also offer "Delete my account". Your clock-in records are part of your employer's records (section 1), so this sends a request to your employer instead of deleting them on the spot:

- Straight away, you are signed out of Taptime on every phone and every browser, and this phone's key is deleted from it.

- Your clock-in records, your requests about them and the history of changes to them are not deleted: they are legal records that your employer keeps and controls (section 9). Your employee record is not deleted or switched off either.

- Your request reaches your employer: it appears, with its date, on your entry in their Taptime dashboard, and the business's owners are told by e-mail. The e-mail gives your name and the date of your request; it does not contain your e-mail address. [DOĞRULA: e-posta yalnız sunucunun e-posta gönderimi açıkken gider (TAPPA_RESET_DELIVERY=email); canlıda açık olduğu yayından önce ölçülmeli.]

- Your employer decides what happens to your data and must answer you within one month of your request, as data protection law requires (GDPR Article 12(3)). Taptime helps your employer answer. [DOĞRULA: md. 12(3)'ün iki aylık uzatma imkânı ve gerekçe bildirimi yazılsın mı; Taptime'ın işverene yardım yükümlülüğü DPA'ya bağlı (Q23 açık); talebi işveren reddederse çalışana ne söylenir (md. 17(3)(b)/(e)).]

- If you still work there, you can use Taptime again later: your employer sends you a new invitation and you activate again.

If your employer has already switched your account off, nothing is signed out (attempts with a switched-off account stay refused and recorded), but your request still reaches your employer.

What the app does not collect. The app collects no biometric data: it never asks for finger scans or face recognition, and none is needed to sign a request. It does not read your location in the background, keeps no location history and uses no geofence. It has no advertising identifier, no analytics or crash-reporting service, no push-notification service and no push token, and no code from another company that tracks you. It does not use the camera: a QR code is read by your phone's own camera app. The permissions it asks for are NFC, internet access, location while you use the app (asked when you tap) and notifications, if you turn on the reminder (on Android also the timer, which is shown as a notification; on iPhone the timer needs no permission).

13. Changes to this notice

When this notice changes, the new version is published here with its date, and earlier versions are kept. [DOĞRULA: değişikliklerin işverenlere/kullanıcılara nasıl duyurulacağı.]
```

## Bu metindeki olguların kaynağı (okundu)

| Cümle | Kaynak |
|---|---|
| Çalışan alanları (ad, lokasyon, departman, unvan, e-posta, tarihler) | `db/migrations/00003` `employees`; dış referans `00037` |
| Oturum çerezi yalnız hash; kaba cihaz etiketi | `00003` `sessions.token_hash`, `device_info`; `internal/handler/device.go` `coarseDevice` |
| Her dokunuşta IP ve (paylaşılmışsa) GPS saklanır | `00005` `transactions.source_ip/gps_lat/gps_lng`; `internal/domain/checkin/checkin.go` (`req.GPS != nil`) |
| Biyometri yok, GPS yalnız basış anında | CLAUDE.md §4.1–4.2; `web/templates/pages/landingview.go` `LandingPrivacyNote` |
| Reklam/analitik çerezi yok, dış gömme yok | `landingview.go` `LegalPages` (cookies lede); çerez tablosu koddan türetilir |
| 150 m, dört kontrol, doğrulanamayan kayıt da yazılır | CLAUDE.md §5, §4.6; `deploy/k8s/05-config.yaml` `TAPPA_GPS_RADIUS_M` |
| Kayıt değişmez, düzeltme yeni kayıt + audit | CLAUDE.md §4.3 |
| Panel kullanıcısı: e-posta, bcrypt hash, oturum hash, audit | `00006`, `00069`, `00018`; `audit_log` (`00005`) |
| Kayıt: işletme adı, VAT, tür, lokasyonlar; VIES; ≤ 24 saat | `00078` `admin_signup_claims` (`ttl_ceiling`, `draft_closed`); `internal/handler/operator/vat.go` |
| Holding ortağında dokunuş | `00046` `site_punches`; ADR 0027 |
| Entegrasyon API'si işletme başına Taptime açar | `web/templates/pages/pricingview.go` `PricingFAQ`; ADR 0028 |
| Ürün içinden operatör erişimi audit'li — yalnız `tappa_operator` rolü için; sunucu/veritabanı yöneticisinin erişimi ayrı ve bu log'a düşmez | CLAUDE.md §4.5; ADR 0020/0021 (`operator_audit_log`); `deploy/k8s/01-rbac.yaml` (`pods/exec` → `tappa-postgres-0`) |
| Hetzner fsn1 (Falkenstein) | `deploy/README.md` başlık ve *"AB kriteri"* |
| AWS SES `eu-central-1` | `deploy/k8s/05-config.yaml` `TAPPA_SMTP_HOST`; open-questions Q02 |
| SES'in taşıdığı e-postalar: davet, kayıt onayı, parola sıfırlama, parola değişti bildirimi, işletme bağlama | ADR 0022; `internal/handler/passwordnotice.go`; `internal/handler/accountlinkbusinesses.go` |
| QR kanalı (plakete basılı) | CLAUDE.md §5 *"QR kanalı"*; `landingview.go` `SourceQRRequiresIP` |
| Cloudflare R2 AB, istemci tarafı şifreli (rclone crypt) | `deploy/README.md` yedek hedefi tablosu (T45); `scripts/pg-backup-ship.sh` |
| Cloudflare yalnız DNS (proxy kapalı) — trafik geçmiyor, bu yüzden listede değil. [DOĞRULA: bu süregiden bir işletim olgusu; her Taptime host'u için DNS-only kalmalı] | **Ölçüldü 2026-11-11T09:15Z (salt-okur):** `dig +short A <host> @1.1.1.1` → `taptime.mt`, `www.taptime.mt`, `api.taptime.mt`, `ops.taptime.mt`, `tappa.everva.com.tr` hepsi `144.76.158.60` (origin, Cloudflare kenar adresi değil); `curl -sI` `https://tappa.everva.com.tr/activate?x=1` (301 → `taptime.mt`), `https://taptime.mt/` (200), `https://www.taptime.mt/` (301) — hiçbirinde `cf-ray` ya da `server: cloudflare` yok. Denetim anında `tappa.everva.com.tr` joker kayıtla proxy'liydi; orkestratör 2026-11-11'de DNS-only A kaydı ekledi. `deploy/README.md` *"1. DNS"* |
| Yedek 30 gün, kova kilidi 14 gün | `deploy/k8s/50-backup.yaml` `BACKUP_RETENTION_DAYS`; `deploy/README.md` *"Kova kilidi"* |
| Saklama süresi kararsız | `deploy/k8s/05-config.yaml` `TAPPA_RETENTION_YEARS` yorumu; open-questions Q13 |
| Log'lar 15 gün | Q28 (b), kullanıcı 2026-10-14 (`2ef582ea`, dal `m10-a1`; bu dalda yok — T285) |

## Bölüm 12'nin kaynağı (okundu, 6fdeaced; e-posta ve iOS sayacı satırları 250bd852)

| Cümle | Kaynak |
|---|---|
| Uygulama isteğe bağlı; tarayıcı yolu aynen | ADR 0045 (`docs/adr/0045-istege-bagli-native-uygulama.md`) |
| Aktivasyon: davet kodu, işveren + onay ekranı, ilk gerçek NFC dokunuşu bitirir, `transactions` satırı yok, öteki oturum/cihazlar kapanır | `internal/handler/appactivate.go` (baş yorum: lookup/bind/complete; *"No transactions row"*; *"revokes the person's browser sessions and app devices (Q40 (a))"*) |
| Anahtar güvenli donanımda, özel yarısı çıkmaz; sunucuda yalnız açık anahtar | `mobile/android/app/src/main/kotlin/mt/taptime/app/device/KeystoreKeys.kt` (StrongBox/TEE, dışa aktarılamaz); `mobile/ios/Taptime/DeviceKey.swift` (`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`); `db/migrations/00108_create_app_devices_and_request_nonces.sql` (`public_key`, 65 bayt) |
| Parola/çerez yok; istek imzalı | `internal/appdevice/appdevice.go`, `request.go`; `mobile/android/app/src/main/kotlin/mt/taptime/app/wire/Wire.kt` (`Headers`: `App-Device-Id`, `App-Signature`, `App-Nonce`) |
| Kaba etiket sunucunun, platformdan; uygulama serbest metin göndermez | `internal/handler/appactivate.go` `appPlatforms` (`"iPhone"`, `"Android"`), `appCompleteRequest` (code, publicKey, platform, sun/liveQR); `00108` `device_label` |
| Etkinleştirme, son kullanım, imzadan çıkma zamanları | `00108` `created_at`, `last_used_at`, `revoked_at`; `db/queries/appdevices.sql` (*"Refreshes last_used_at on use"*) |
| User-Agent ayarlanmıyor (platform varsayılanı gider) | `mobile/android/app/src/main/kotlin/mt/taptime/app/wire/Transport.kt` ve `mobile/ios/Taptime/AppClient.swift`'te `User-Agent` yok (grep); sunucu tarafında app yüzeyi UA okumaz (`internal/handler/app*.go`) |
| Telefonda tutulanlar: anahtar, cihaz kimliği, ad, anahtarlar; Android'de yedek/aktarım kapalı | `PrefsDeviceState.kt` (dört değer); `mobile/ios/Taptime/ActivationStore.swift` (Keychain, `kSecAttrSynchronizable=false`); `StaffNotifications.kt` `PrefsStaffSwitches`; `mobile/ios/Taptime/StaffCopy.swift` `staff.remind`; `AndroidManifest.xml` `allowBackup=false` + `dataExtractionRules` |
| Konum yalnız basışta, tek okuma, ön planda; başlangıçta sorulmaz | `AndroidManifest.xml` (*"asked for at the tap only (M16-13), never at start"*; arka plan izni yok); `mobile/ios/Taptime/Info.plist` `NSLocationWhenInUseUsageDescription`; `mobile/ios/Taptime/TapFlow.swift` (basış başına tek `requestLocation`); `mobile/android/app/src/main/kotlin/mt/taptime/app/tap/TapLocation.kt`, `Fix.kt` |
| Konum kayıtla saklanır, iz değil | `internal/handler/apptap.go` `appTapRequest` (`lat`/`lng` tek okuma), `appFix`; `internal/domain/checkin/checkin.go` `GPS` (*"read ONCE, at the moment of the tap"*); `00005` `transactions.gps_lat/gps_lng` |
| Konum izni yoksa dokunuş yine kaydedilir | `AndroidManifest.xml` `uses-feature android.hardware.location required=false` yorumu; CLAUDE.md §5 satır 6–7 |
| Sahte konum bayrağı yalnız `true` ve yalnız okuma ile gider; kaydın karar girdilerinde kalır; varsayılan inceleme, işveren kapatabilir | `wire/Wire.kt` `tap()` (*"ONLY WHEN TRUE"*); `mobile/ios/Taptime/TapFlow.swift` (`isSimulatedBySoftware`, satır 135–147, 417); `internal/domain/tap/decide.go` (`tap:mockLocation`); `00008` `transactions.policy_context`; `internal/policy/baseline.go` `base:mock-location-review` (*"the report is still kept on the record"*) |
| Cihaz doğrulaması YOK (köşeli parantez) | `mobile/ios/Taptime/Taptime.entitlements` (*"App Attest … stays OUT until Phase E"*); `mobile/android/app/build.gradle.kts` (*"no Play Integrity (Faz E)"*); `internal/appdevice/appdevice.go:19` (*"No attestation"*) |
| E-postayla giriş YOK (köşeli parantez) | `internal/handler/app*.go`'da e-posta rotası yok; `wire/Wire.kt` `Paths`'te yok; `docs/plan/m16-mobil.md` M16-35 yapılmamış; ADR 0049 |
| Saatlerim: varsayılan kapalı, 1/2/4/13 hafta, koordinat/IP/güven puanı yok, yazma yok | `db/migrations/00117_add_the_staff_hours_window.sql` (NULL = kapalı; `IN (1, 2, 4, 13)`); `internal/domain/ledger/ownhours.go` (*"No coordinate, address, trust or note is read"*; `OwnRecord` alanları; *"NOTHING IS WRITTEN"*); `internal/handler/apphours.go` |
| Saatlerim telefonda tutulmaz | Telefondaki tüm kalıcı depolar (grep `getSharedPreferences`/`UserDefaults`): yalnız cihaz durumu, widget, iki anahtar; `PresenceWidget.kt` (*"My hours stay off the device"*) |
| Olağan vardiya yalnız sahibi işaretlerse | `db/migrations/00116_add_the_staff_shift_switch.sql` (`staff_shift_visible DEFAULT false`) |
| Eksik zaman talebi: `punch_claims` satırı, kayıt değil; saat yalnız onayda, manuel kayıt olarak; ret bir şey eklemez; değişmez | `internal/domain/claim/claim.go` (*"A CLAIM IS NOT A RECORD"*); `db/migrations/00068_create_punch_claims_and_their_decisions.sql` (`kind`, `claimed_at`, `created_at`; `punch_claims_no_mutation`); `internal/handler/appclaims.go` |
| Hatırlatma, kilit ekranı sayacı, widget: yerel, push yok, konum yok, varsayılan kapalı; yalnız zaman gösterir | `mobile/android/app/src/main/kotlin/mt/taptime/app/staff/PresenceSync.kt` (*"NO PUSH, NO LOCATION, NO BACKGROUND WORK"*); `StaffNotifications.kt` (*"No name, no business, no venue"*; anahtarlar `false` varsayılan); `PresenceWidget.kt` (iki an + bir saat metni); `mobile/ios/Taptime/StaffCopy.swift` `StaffNotifier` (*"No push, no location, nothing leaves the phone"*); `AndroidManifest.xml` `POST_NOTIFICATIONS` yorumu |
| Sayaç ve widget iki platformda | iOS: `mobile/ios/TaptimeWidgets/` (WidgetKit + Live Activity), `mobile/ios/Shared/PresenceShare.swift` (App Group'ta yalnız anlar + iki HH:MM metni; `PresenceSyncTests` anahtar kümesini sabitler), `mobile/ios/Taptime/PresenceSync.swift` (push yok, yerel `Activity.request`) |
| "Remove this phone" | `wire/Wire.kt` `REVOKE`; `mobile/android/app/src/main/kotlin/mt/taptime/app/staff/StaffFlow.kt` `signOut`; `mobile/ios/Taptime/StaffModel.swift:206` |
| Silme talebi: her oturum/cihaz kapanır; kayıtlar, talepler, audit ve çalışan satırı dokunulmaz; deaktive kişide hiçbir şey kapanmaz ama talep işverene gider | `internal/domain/tenant/staffdeletion.go` (baş yorum ve `requestDeletionOnce`); `internal/handler/appaccount.go`; telefonda anahtar silinir: `StaffFlow.kt` `deleteAccount` (`forget`), `mobile/ios/Taptime/StaffModel.swift:207` |
| Talep panelde, tarihiyle | `internal/domain/tenant/staffsummary.go` `deletionRequested`; `internal/handler/employees.go:934` (*"Asked to delete their account on …"*) |
| Sahiplere e-posta, adressiz gövde | `internal/handler/ownernotice.go` (`deletion_request` türü; audit `employee.deletion_notice.sent`/`.undelivered`, detail'de adres yok); `internal/handler/appaccount_db_test.go` [G37-d] |
| Bir ay | Panelin işverene söylediği: `web/templates/components/roster.templ:189` (*"reply to them within one month"*); süre hukuki — GDPR md. 12(3) |
| Yeniden etkinleştirme yeni davetle | Çalışan satırı silinmez/deaktive edilmez (`staffdeletion.go`); aktivasyon davet koduyla (`appactivate.go`); M16-38 onay metni (*"If you still work there, you can sign in again later."*) |
| Biyometri yok | Anahtar kullanıcı doğrulaması istemez (`KeystoreKeys.kt`, `DeviceKey.swift`'te `setUserAuthenticationRequired`/`LAContext`/`biometry` yok — redline R1 mobil kalıpları); CLAUDE.md §4.1 |
| Reklam kimliği, analitik, çökme raporu, push yok; kamera yok | `build.gradle.kts` bağımlılıkları (yalnız Compose BOM, ui, foundation, activity-compose); iOS projesinde Swift paketi yok (`XCRemoteSwiftPackageReference` 0); `Taptime.entitlements` (`aps-environment` yok); `mobile/android/app/src/test/kotlin/mt/taptime/app/MergedManifestTest.kt` (Firebase yok; izin listesi kapalı); `AndroidManifest.xml` ve `Info.plist`'te kamera izni yok |
| İzinler: NFC, internet, konum (kullanırken), bildirim | `AndroidManifest.xml` (NFC, INTERNET, ACCESS_FINE/COARSE_LOCATION, POST_NOTIFICATIONS); `Info.plist` (`NFCReaderUsageDescription`, `NSLocationWhenInUseUsageDescription`); iOS bildirimi anahtar açılınca sorulur (`StaffCopy.swift` `StaffNotifier.ask`) |

Listede bilerek **olmayanlar:** Tailscale (yalnız yöneticinin sunucuya ağ erişimi; uygulama verisini
taşımaz — [DOĞRULA]), GitHub ve Docker Hub (kod ve imaj; kişisel veri yok), Telegram ve e-posta
bildirimleri (healthchecks.io'nun operatöre ilettiği uyarılar; içerik toplam sayı — [DOĞRULA], healthchecks.io maddesiyle aynı:
kişisel veri taşımadığı teyit edilmeli).
