# ADR 0025 — Aktivasyon ilk NFC dokunuşunda tamamlanır; practice tap kalkar

- **Durum:** kabul edildi
- **Tarih:** 2026-10-04
- **Karar veren:** kullanıcı (ürün sahibi). Kararlar: aktivasyon yalnız fiziksel
  NFC dokunuşunda tamamlanır · işverenin **herhangi bir** aktif plaketi yeter ·
  aktivasyon çerezi **davetin süresi bitene kadar** yaşar · practice tap'in yerini
  aktivasyon dokunuşu alır.
- **Etkilenen:** migration `00030_add_consent_to_employee_invites.sql` ·
  [`db/queries/invites.sql`](../../db/queries/invites.sql) ·
  [`internal/invite`](../../internal/invite) (`RecordConsent`, `Binding`,
  `Activate(code, binding)`, `ErrConsentMissing`) ·
  [`internal/handler/activate.go`](../../internal/handler/activate.go)
  (`Submit`, `Pending`, `CompleteByTap`, `finishActivation`, `Status`) ·
  [`internal/handler/tap.go`](../../internal/handler/tap.go) ·
  [`internal/handler/cookies.go`](../../internal/handler/cookies.go) ·
  [`internal/domain/tap/decide.go`](../../internal/domain/tap/decide.go) ·
  `web/templates/pages/activate.templ` · `web/static/js/activate.js`
- **İlgili:** ADR 0003 (SDM/SUN) · ADR 0008 (practice satırı ve yön zinciri — tarihsel
  satırlar için geçerli kalır) · ADR 0010 · CLAUDE.md §4.4, §4.5, §4.6, §5, §9

---

## Neden bir ADR

Oturum **verme sınırı** değişti: oturum artık bir form POST'unda değil, bir
GET'te (plaketin açtığı `/t`) doğuyor; ve §5'in "Practice tap" kuralı kalktı.
İkisi de CLAUDE.md §10'un "güvenlik sınırı / karar motoru değişti → ADR" maddesi.

## Karar

1. **Davet linki bir sihirbaz açar** (`GET /activate?step=1..4`, JS'siz çalışır):
   hoş geldin · gizlilik bildirimi + onay kutusu (tek form) · hazırlan (Wi-Fi,
   "bu tarayıcıda kal") · "şimdi plakete dokun" bekleme ekranı.
2. **Onay POST'u (`/api/activate`) hiçbir şey TÜKETMEZ ve oturum VERMEZ.** Davete
   `consented_at` + `consent_binding_hash` yazar (`RecordInviteConsent`); bağ
   (binding), yalnız bu tarayıcının HttpOnly aktivasyon çerezinde duran 256-bit
   rastgele bir değerin HMAC'idir. Çerez `<csrf>.<code>.<binding>` olur.
3. **İlk gerçek NFC dokunuşu aktivasyonu tamamlar.** `GET /t`, tarayıcıda onaylı
   (bağlı) bir aktivasyon varsa isteği `Activation.CompleteByTap`'e verir — oturum
   olsun olmasın (onay adımı başka çalışanın oturumunu devralmayı zaten açıkça
   onaylattı). Sıra: davet hâlâ geçerli mi → kanal NFC mi (QR aktive edemez) →
   **`sun.Verify`** (önce CMAC, sonra atomik `ctr` ilerletmesi, §4.4) → plaket
   `active`, duvarda ve **davetle aynı tenant'ta** → `ConsumeInviteAndActivate`
   (onay **ve** bağ artık bu tek ifadenin WHERE'inde) → ikinci cihazsa önce iptal,
   sonra oturum → çerez değişimi → `activation.completed` (plaketin uid'i ve
   lokasyonu ile). Bu dokunuş **`transactions` satırı yazmaz** (§5 satır 3 gibi:
   yoklama değil).
4. **Practice tap kalkar.** `Decide` artık `Practice` set etmez. Kolon ve tarihsel
   satırlar kalır (§4.3 değişmezlik; ADR 0008'in "practice satırı açık girişi
   saklamaz" kuralı tarihsel satırlar için yürürlükte).
5. **Aktivasyon çerezi davetin `expires_at`'ine kadar yaşar** (üst sınır 30 gün).
6. **`GET /activate/status`** (`waiting|done|none`, no-store, kendi oran bütçesi):
   bekleme sekmesi (`activate.js`) aktivasyonun diğer sekmede bittiğini görür.
7. Aktivasyon ekranları tap CSP'sini + `connect-src 'self'` taşır.
8. `/activate/tour` ve `/activate/done` kaldırıldı.

## Güvenlik değerlendirmesi

- **Ekilmiş çerez (cookies.go ölçü 3) aktive edemez.** Siteler arası bir GET
  kurbanın tarayıcısına bir **kod** ekleyebilir ama **bağ** ekleyemez: bağ yalnız
  CSRF jetonu doğrulanmış onay POST'unda üretilir. Bağsız çerez "bekleyen" sayılmaz,
  dokunuş §5 satır 3'e (sihirbaza) düşer. Bağ sunucuda saklandığı için başka bir
  tarayıcıdan verilen sonraki onay onu **taşır**; eski tarayıcının dokunuşu
  `ErrConsentMissing` ile reddedilir ve hiçbir şey tüketilmez.
- **Replay:** aktivasyon dokunuşu `sun.Verify`'ın kendisidir; aynı `(tag, ctr)` ile
  N eşzamanlı istek → tam 1 ilerletme, aynı davetle N farklı geçerli dokunuş → tam 1
  oturum (tek ifadeli tüketim). İkisi de `-race` ile test altında.
- **Tenant:** yalnız davetin tenant'ına ait aktif plaket kabul edilir; yabancı
  tenant'ın uid'i bu tenant'ın audit izine **yazılmaz**.
- **Bedel 1 — GET ile durum değişimi.** `/t` artık (yalnız bağlı tarayıcıda) sayaç
  ilerletir ve oturum verir. Plaket URL'si zaten tek kullanımlıktır (sayaç), ön-
  getirme (prefetch) en kötü ihtimalle o dokunuşu harcar; çalışan yeniden dokunur.
- **Bedel 2 — çerez günlerce yaşar.** Kilitsiz bırakılmış ortak bir telefonda
  bekleyen bir aktivasyon kalabilir; tamamlamak yine de işverenin plaketine fiziksel
  dokunuş ister ve başarıda çerez silinir.
- **Bedel 3 — QR ile aktivasyon YOK.** Arka planda NFC okuyamayan telefonlar (iPhone
  X ve öncesi) bugün aktive **olamaz**. QR fiziksel dokunuş kanıtı taşımadığı için
  bilinçli; çözüm (ör. müdür onaylı aktivasyon) ayrı bir ürün kararıdır.

## Test edilenler (adlarıyla)

`TestSubmit_RecordsConsentAndIssuesNoSession` · `TestTap_CompletesAConsentedActivation` ·
`TestTap_RefusalsActivateNothing` · `TestTap_WithoutConsentIsNotPending` ·
`TestE2E_ActivationFlow` · `TestE2E_ReplayedSUNCannotActivate` ·
`TestE2E_ConcurrentActivatingTapsProduceExactlyOneSession` ·
`TestE2E_ForeignTenantPlaqueCannotActivate` · `TestE2E_DeadPlaqueCannotActivate` ·
`TestE2E_ExpiredInvitationCannotActivate` · `TestE2E_ConsentWithoutATapActivatesNothing` ·
`TestE2E_SecondDeviceRevokesTheFirst` · `TestStatus_ReportsTheStateWithoutNamingAnybody` ·
`TestConsumeInvite_RequiresConsentFromTheSameBinding` ·
`TestDecide_FirstTapAfterActivationIsNotPractice` · `TestDecide_NoNewRecordIsEverPractice`.

**Not — test tarafında SDM MAC üretimi.** E2E testleri gerçek MAC'li URL ister;
`internal/handler/sunurl_test.go` bunu AN12196 KAT'ına sabitlenmiş bağımsız bir
test yardımcısıyla üretir. Bu, check-in yolunda reddedilen "ikinci SDM
implementasyonu" duruşunun bilinçli ve dar bir istisnasıdır (yalnız `_test.go`).
