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
   **ilerletmeyen ön kontrol** (`PreviewWithoutReplayProtection`): plaket **davetle
   aynı tenant'ta**, `active` ve duvarda mı — değilse HİÇBİR sayaç ilerlemeden ret
   (denetim R5) → **`sun.Verify`** (önce CMAC, sonra atomik `ctr`, §4.4) →
   `ConsumeInviteAndActivate`
   (onay **ve** bağ artık bu tek ifadenin WHERE'inde) → ikinci cihazsa önce iptal,
   sonra oturum → çerez değişimi → `activation.completed` (plaketin uid'i ve
   lokasyonu ile). Bu dokunuş **`transactions` satırı yazmaz** (§5 satır 3 gibi:
   yoklama değil).
4. **Practice tap kalkar.** `Decide` artık `Practice` set etmez. Kolon ve tarihsel
   satırlar kalır (§4.3 değişmezlik; ADR 0008'in "practice satırı açık girişi
   saklamaz" kuralı tarihsel satırlar için yürürlükte).
5. **Aktivasyon çerezi davetin `expires_at`'ine kadar yaşar** (üst sınır 30 gün).
6. **`GET /activate/complete`** başarı sayfası; **`GET /activate/status`** (`waiting|done|none`, no-store, kendi oran bütçesi):
   bekleme sekmesi (`activate.js`) aktivasyonun diğer sekmede bittiğini görür.
   `done` yalnız aktivasyon dokunuşunun bıraktığı `tappa_activated` işaret çerezi
   (15 dk, değeri verilen oturumun id'si) tarayıcının canlı oturumunu adlandırıyorsa
   döner; başka bir canlı oturum (devredilen telefon) `none`'dır.
7. **İniş metinleri:** oturumsuz bir dokunuş `/activate?from=tap`'e gider ve linki
   **bu tarayıcıda** açması söylenir (uygulama-içi tarayıcı vakası; link hâlâ
   geçerli). Kurulu bir telefon `/activate`'i yeniler ya da harcanmış linkini açarsa
   "This phone is already set up" görür. Canlı oturumlu telefonda QR taraması
   bekleyen aktivasyona rağmen sıradan check-in kalır (yalnız NFC devralır, R6).
8. Aktivasyon ekranları tap CSP'sini + `connect-src 'self'` taşır.
9. `/activate/tour` ve `/activate/done` kaldırıldı.

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
- **Tenant:** yalnız davetin tenant'ına ait aktif plaket kabul edilir ve bu,
  **sayaç ilerlemeden önce** ilerletmeyen ön kontrolde sınanır — B tenant'ının
  plaketine dokunan A davetlisi B'nin `last_ctr`'ını oynatamaz. Yabancı tenant'ın
  uid'i bu tenant'ın audit izine **yazılmaz**. Bilinmeyen uid, yabancı plaket ve
  sahte imza **aynı ekranı ve durum kodunu** alır (§4.7, kehanet yok).
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

**Not — SDM MAC üretimi.** E2E testleri gerçek MAC'li URL ister. 2026-10-07'den
beri bu üretim `internal/sun/mint.go`'da (`MintTapPath`, AN12196 KAT'ına sabit,
`mint_test.go`); `internal/handler/sunurl_test.go` artık onu çağırır, test tarafındaki
ikinci kopya kalktı.

## Denetim 2. tur düzeltmeleri (2026-10-04)

- **Başarı kendi sayfasında:** aktivasyon dokunuşu `/t?…` adresinde onay
  göstermez; `303 → /activate/complete` (`tappa_activated` işaretiyle kapılı,
  yenilemede aynı). Eski hâl, yenilemede harcanmış sayacı butonlu tap sayfası olarak
  açıyordu; buton yalnız `sys:sun-invalid` reddi yazıp gerçek dokunuşu 60 sn
  debounce'a sokuyordu. **Yapılmayan (§5'e dokunduğu için):** canlı oturumla
  harcanmış bir sayacın önizlemesinde butonsuz sayfa göstermek — bugün o buton
  §4.6 gereği kaydedilen bir reddi üretiyor; davranışı değiştirmek ayrı karar.
- **İşaret temizliği:** link açmak ve onay vermek eski `tappa_activated`'ı siler;
  bu tarayıcıda başka bir aktivasyon çerezi varken `status` asla `done` demez.
- **Dürüst inişler:** iptal edilmiş oturumla dokunuş `/activate?from=signedout`
  ("This phone was signed out"); `from=tap` metni linkin hâlâ geçerli olduğunu vaat
  etmez.
- **Hizmet dışı plaket** de aynı genel reddi alır (durumu imzadan önce bilinir).
- **Deaktive çalışanın canlı oturumu** bekleyen aktivasyona rağmen sıradan tap
  sayfasında kalır → §5 satır 4 kaydı + güvenlik uyarısı. **Bedeli:** ayrılmış bir
  çalışanın oturumunu taşıyan telefonu yeni çalışana DOKUNUŞLA devretmek mümkün
  değil — yeni çalışan başka bir tarayıcıda kurmalı ya da çerezler silinmeli. Ürün
  kararı bekliyor.

## Birleştirme sırası (zorunlu)

`origin/m10-a1` migration **00026–00029** ve ADR **0020–0024**'ü kullanıyor; bu iş
o yüzden **00030** ve **0025** aldı. **`m10-a1` önce `main`'e birleşmeli.** Bu dal
önce birleşirse üretim 00030'a çıkar ve 00026–00029 sonradan "eksik" kalır: goose
`up` onları **`-allow-missing` olmadan uygulamaz**. Ayrıca
`cmd/tappa/storekeyshape_test.go`'daki sorgu sayısı iki dal birleşince yeniden
hesaplanır (bu dal +2).

## Geliştirme aracı (2026-10-07)

Masaüstü tarayıcı plakete dokunamaz; aktivasyon ise yalnız gerçek NFC dokunuşunda
tamamlanır. **`POST /dev/simulate-tap`** doğrulamayı ATLAMAZ: bekleyen aktivasyonun
(yoksa canlı oturumun) işverenine ait aktif, duvardaki bir plaketi seçer (çalışanın
kendi lokasyonu öncelikli; isteğe bağlı `tag`), `Verifier.MintNextTapForDevelopment`
ile **`last_ctr+1`** okumasının URL'sini KEK'le açılan plaket anahtarıyla üretir
(`internal/sun/mint.go`; anahtar ve MAC loglanmaz, anahtar `Zero`'lanır) ve tarayıcıyı
`/t?…`'ye 303'ler. Sonrası gerçek dokunuşla aynıdır: `CompleteByTap` → `sun.Verify` →
atomik ilerletme; canlı oturumla sıradan tap sayfası.

**Kapı — üç kat:** (1) `cmd/tappa/main.go` aracı yalnız `handler.DevToolsEnabled(cfg)`
iken kurar ve router'a ekler; (2) `DevTap.Mount` aksi hâlde hiç rota kaydetmez;
(3) `DevTap.Simulate` her istekte yeniden 404 verir. `DevToolsEnabled` =
`TAPPA_ENV=dev` **VE** `TAPPA_BASE_URL` loopback (localhost / 127.0.0.0/8 / ::1) —
yalnız `dev` yetmez, çünkü boş `TAPPA_ENV` dev'e düşer. Üretim (`deploy/k8s`:
`TAPPA_ENV=prod`, `https://taptime.mt`) iki koşulu da sağlamaz. Form `Origin`
(yoksa `Sec-Fetch-Site: same-origin`) ister. Ekranlardaki kesikli **DEV ONLY** şeridi
yalnız `EnableDevTools` çağrılmış (ve kapıyı geçen) bir dağıtımda görünür: bekleme
ekranı (yeni sekme), `/activate/complete`, "already set up", sonuç sayfası. Tap
sayfasında yok. Testler: `TestDevToolsEnabled_IsDevOnALoopbackAddressOnly`,
`TestDevTap_DoesNotExistOutsideDevelopment`, `TestDevStrip_OnlyOnADevelopmentDeployment`,
`TestDevTapDB_ASimulatedTapActivatesThroughTheRealPath`.
