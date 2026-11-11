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

12. Changes to this notice

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

Listede bilerek **olmayanlar:** Tailscale (yalnız yöneticinin sunucuya ağ erişimi; uygulama verisini
taşımaz — [DOĞRULA]), GitHub ve Docker Hub (kod ve imaj; kişisel veri yok), Telegram ve e-posta
bildirimleri (healthchecks.io'nun operatöre ilettiği uyarılar; içerik toplam sayı — [DOĞRULA], healthchecks.io maddesiyle aynı:
kişisel veri taşımadığı teyit edilmeli).
