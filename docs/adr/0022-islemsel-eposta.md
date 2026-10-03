# ADR 0022 — İşlemsel e-posta: AWS SES SMTP arayüzü + stdlib `net/smtp` (STARTTLS 587, eu-central-1), metin taşımayan gönderim hatası, istek yolundan çıkan sıfırlama gönderimi

- **Durum:** kabul edildi — E-posta kararları **K1** (`net/smtp`), **K2** (akış başına config),
  **K3** (adresi olmayan çalışan için owner-only "linki göster" yedeği), **K4** (yalnız
  İngilizce), **K5** (sabit Taptime gönderen), **K6** (`mail.taptime.mt`), **K7**
  (`eu-central-1`), **K8** (bounce/complaint pilot sonrası), **K10** (SES TLS Require),
  **K11** (signup doğrulaması ayrı, pilot sonrası), **K12** ("parolanız değişti" bildirimi),
  **K13** (panel yedeği kalır) — *"✅ önerisiyle uygulanır"*
  ([m10-platform.md](../plan/m10-platform.md) §6). **EM-K9 — kullanıcı kararı (2026-10-03),
  birebir:** *"hayir sahte birsey eklemene gerek yok ses'e baglariz direkt"* → yerel posta
  yakalayıcı (Mailpit) **yok** (§12). Bu ADR [open-questions.md](../plan/open-questions.md)
  **Q02**'nin cevabıdır.
  **Uygulama:** HEAD `f3c9c04`'ün izlenen dosyalarında üretim kodunda `net/smtp` ya da
  `net/mail` import eden dosya 0 (Bağlam, B1 — o âna tarihli). EM-2'nin `internal/mail`'i
  ayrı bir worktree'de yazıldı; 4. turda (kapanış) orkestratör güncel kopyasını bu worktree'ye
  **takip dışı** olarak koydu (birleşik ağaç denetimi) — ADR'deki `internal/mail` test adları
  o kopyadaki adlardır ve `TestEveryNamedTestExists` onlara karşı doğrulandı; birleştirmede
  EM-2'nin kendi ağacı geçerlidir. EM-2'nin sapmaları orkestratörce kabul edildi ve §2–§4'e
  normatif olarak işlendi (2. tur, 2026-10-03); 3. turda orkestratörün iki yeni kuralı (bayt
  tavanı, yankı penceresi — sapma j, k) ve EM-2'nin 2. tur kararları eklendi; **eşitleme turunda
  (aynı gün) EM-2'nin teslim edilen 2. tur koduna eşitlendi** (yankı listesi, `Credential`,
  `Message` redaksiyonu, tavanın gerekçesi, sınıf eşlemesi, EM-2'nin sayılı sınırları). Uygulama EM-2…EM-9; EM-10/EM-11/EM-12 kendi
  ADR'leriyle. **Bugünkü davranış bu ADR'yle değişmez:** sıfırlama `none`, davet panel kanalı.
- **Tarih:** 2026-10-03 (2. tur aynı gün: üçüncü göz ve güvenlik denetimi bulguları, EM-K9
  kararı, EM-2 sapmaları; 3. tur aynı gün: bayt tavanı, yankı penceresi, kapanış pininin
  yeniden tarifi, dev'de `email` modu, DKIM imzalı içerik, kök havuzunun ortam yolları)
- **Bağlam:** [M10 Akış B](../plan/m10-platform.md) §4, görev EM-1. Ölçüm komutları ve sapma
  listesi: aynı dosya → *"Kart düzeltmesi (2026-10-03, EM-1 uygulaması sırasında)"*.
- **İlgili:** [ADR 0005](0005-kabul-edilen-riskler.md) (Y-D — bu ADR'yle tarihli ek, §5) ·
  [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) (durum notu) ·
  [ADR 0013](0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md) (c) (signup e-posta doğrulaması —
  EM-11) · [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) §8 (e-postada tenant adı —
  WL-11) · [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) (operatör yüzeyi e-posta
  göndermez; *"Kriptografinin yeri"* kararı) · CLAUDE.md §1, §3, §4.6, §7, §10

**Numara notu.** 0022 bu ADR'ye ayrılmıştı; 0023 ve 0024 ondan önce yazıldı (akış sırası
2026-10-02'de A1 → C → B → A2 oldu — ADR 0023 *"Numara notu"*). Numara değişmedi.

## Neden bir ADR

Ürün ilk kez bir **üçüncü taraf işleyiciye** (AWS SES) kişisel veri (alıcı adresi, çalışan ve
işletme adı) ve **taşıyıcı kimlik bilgisi** (davet kodu ve sıfırlama token'ı taşıyan link)
gönderecek. Üç güvenlik sınırı değişir: (1) bir sır süreçten çıkıp bir posta kutusuna gider;
(2) yeni bir kimlik bilgisi (SMTP) Secret'a girer; (3) **bizim kontrol etmediğimiz bir metin**
(SMTP sunucusunun yanıtı) hata zincirimize girer — ve o metin bizim sırlarımızı alıntılayabilir.
CLAUDE.md §10: güvenlik sınırı değişince ADR. Q02 2026-07'den beri açıktı ve üç işi bloke
ediyordu: M5-02'nin "kod çalışanın kendi kanalına" adımı (ADR 0005 Y-D), M7-04'ün sevk edilen
yapılandırmada ölü üç kriteri ve M7-07.

Bu ADR **sözleşmeyi** yazar, kod yazmaz. Normatif kurallar onları uygulayacak EM görevine
**→** işaretiyle bağlanır.

## Bağlam — bugün (ölçüldü, HEAD `f3c9c04`)

| # | Olgu | Kaynak / ölçüm |
|---|---|---|
| B1 | Üretim Go kodunda `net/smtp` import eden dosya **0**, `net/mail` import eden dosya **0** (HEAD `f3c9c04`'ün izlenen dosyaları; 4. turdan beri bu worktree'de EM-2'nin takip dışı `internal/mail` kopyası var) | `rg -c '"net/smtp"' --glob '*.go' internal cmd web` → çıkış 1; aynısı `"net/mail"` ile → çıkış 1 (kopyadan önce) |
| B2 | `TAPPA_RESET_DELIVERY` kapalı küme `{none}`; boş = `none`; bilinmeyen değer boot hatası ve hata metni **Q02'yi adlandırır** | `internal/config/config.go:279`, `:814`, `:824-831`; `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed` (`config_test.go:445`) `"Q02"` alt dizgesini arar (`:475`) |
| B3 | ConfigMap `TAPPA_RESET_DELIVERY: "none"`; yorumu Q02'yi "cevapsız" diye anar | `deploy/k8s/05-config.yaml:101-106` |
| B4 | `main.go`'da `none` → `resetChannel = nil`; başka her değer `return fmt.Errorf(… nothing implements it …)` | `cmd/tappa/main.go:610-620` (`f3c9c04`'te; dalın ucu `e0f53b6`'da `:626-636` — `git show e0f53b6:cmd/tappa/main.go` ile okundu) |
| B5 | Sıfırlama: önce gönder, sonra **tek** audit satırı (`admin.recovery.requested` ya da `admin.recovery.undelivered`; B8'in bütçesi içinde); gönderim hatasında log'a yalnız `ip`, `admin_user_id`, `err_type` (`%T`) yazılır — kanalın metni değil. Gönderim ve audit **aynı ctx**'i kullanır | `internal/handler/adminreset.go:522-596` (log satırı `:574-575`); pin `TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText` |
| B6 | O kararın ölçülen gerekçesi: kanalın metnini temizlemek (denylist) altı sıradan biçimin ikisinde sızdı (base64 geri yansıtma, büyük harfli adres) | `adminreset.go:537-566` (yorum, güvenlik denetimi ölçümü) |
| B7 | Sıfırlama isteği 250 ms tabana tutulur; yorum, gerçek bir posta taşıyıcısının bu tabanı aşıp kayıtlı/kayıtsız adresi zamanlamayla ayırt ettireceğini ve ardılın *"gönderimi istek yolundan çıkarmak"* olduğunu yazar | `internal/handler/adminresetlimits.go:308-317`; mevcut `TestAdminReset_TimingIsFlat` 40 ms ihraç + 15 ms kanal gecikmesiyle |
| B8 | Hesap başına audit bütçesi: `requested`/`undelivered` satırları yönetici başına 10 dakikada **10** ile sınırlı; aşınca **bir** `admin.recovery.rate_limited` satırı, sonra hiçbir şey — **hiçbir isteği reddetmez** | `adminresetlimits.go:168-228`; `adminreset.go:927-950` (`recordForAdmin`) |
| B9 | Sıfırlama yayılımı: kaynak adres başına 10 dakikada **20** istek; bir istek bir adresi en çok `ResetWindow = MaxCandidates = 8` kimliğe çözer; link ömrü `ResetTTL = 1 saat` | `adminresetlimits.go:118-119`; `internal/adminauth/resetrequest.go:68`, `manager.go:118`, `reset.go:119` |
| B10 | Davet: üretimdeki **tek** `invite.Channel` `ManagerVisibleChannel`'dır (panel); audit eylemi `invite.code_shown_to_manager`, detail `channel: "manager_panel"`. `TAPPA_INVITE_DELIVERY` diye bir değişken **yok** | `internal/handler/employeeactions.go:385`; `internal/invite/channel.go:85`, `:137-154`; `rg -n 'TAPPA_INVITE_DELIVERY' --glob '!docs/**' .` → 0 |
| B11 | Davet hata yolu: `IssueAndDeliver` kanal hatasını `%w` ile sarar ve panel **zincirin tamamını** loglar | `internal/invite/manager.go:310` (`fmt.Errorf("invite: deliver %s: %w", inv.ID, err)`); `employeeactions.go:419` (`"err", err`) |
| B12 | Davet satırı ve kardeşlerin emekliye ayrılması teslimattan **önce** commit edilir; teslimat hatası çalışanı eski linki ölmüş, yenisi gösterilmemiş hâlde bırakabilir — yorumda *"COUNTED, NOT CLOSED"*, sonraki basışta kendiliğinden düzelir | `employeeactions.go:396-418` |
| B13 | Teslimat yükümlülükleri iki yerde yazılı: link loglanmaz, **kalıcı olarak saklanmaz**, tek alıcıya gider. İki taşıyıcı yapı (`invite.Delivery`, `ResetDelivery`) linki düz `string` alanında taşır ve bunu **beyan edilmiş istisna** olarak yazar | `internal/invite/channel.go:12-35`; `adminreset.go:171-187` |
| B14 | `employees.email` `citext`, tenant içinde tekil; saklama kuralı **zayıf**: tek `@`, boşluk yok, ≤254 rune, ASCII dışı serbest. `CancelPendingInvitesForEmployee` var | `db/migrations/00003_create_employees_sessions.sql:53`, `:83-84`; `internal/domain/tenant/staff.go:670-693`; `db/queries/invites.sql:274` |
| B15 | `admin_users.email` `citext`, tenant içinde tekil | `00006_create_admin_users.sql:59`, `:96-97` |
| B16 | Davet ömrü varsayılanı 7 gün | `internal/invite/manager.go:125` |
| B17 | Kayıt: kaynak adres başına saatte **3** tenant; **ömür boyu tavan yok** | `internal/handler/signupratelimit.go:145-146` |
| B18 | Kapanış bütçesi: `httpShutdownGrace` 20 s · `encode.DefaultCloseGrace` 5 s · `terminationGracePeriodSeconds` 30; marj 30 − (20 + 5) = 5 s, testin istediği marja (`DefaultCloseGrace`) eşit. **Test yalnız bu iki sabiti ve YAML'ı okur**: `main.go`'ya eklenen yeni bir bekleme o toplama kendiliğinden girmez. Üçüncü göz ölçtü (2026-10-03): `main.go`'da Shutdown'dan sonra `time.Sleep(3s)` → üç kapanış testi **PASS** — oysa gerçek marj 2 s olur, istenen 5 s. Mevcut yuvalanma testleri de yalnız adlandırılmış sabitleri karşılaştırır | `cmd/tappa/main.go:82`; `internal/encode/session.go:637`; `deploy/k8s/20-app.yaml:80`; `TestShutdownBudget_TheTwoGoWaitsFitInsideTheKubernetesGrace` (`shutdownbudget_test.go:34`); emsaller `TestShutdownBudget_TheDetachedRepairsNestInsideTheHTTPGrace`, `TestShutdownBudget_TheRefusalRecordNestsInsideTheHTTPGrace` |
| B19 | `http.Server.Shutdown`, `RegisterOnShutdown` ile kaydedilen işlevleri `go f()` ile başlatır ve **beklemez** | Go 1.27.1 `net/http/server.go:3257-3259`, `:3290-3297` |
| B20 | Secret anahtar anahtar enjekte edilir; `optional: true` emsalleri `TAPPA_TAG_KEK_PREVIOUS` ve dört `TAPPA_OPERATOR_*` | `deploy/k8s/20-app.yaml:309-424` |
| B21 | Paketleme testi `internal/config/config.go`'daki her `"TAPPA_…"`/`"DATABASE_…"` dizge sabitini okur ve hizmet konteynerinin env'inde ya da ConfigMap'te ister | `cmd/tappa/packaging_test.go:810` (`TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest`) |
| B22 | `config.Config` sırları ham `[]byte`/`string` tutar; yapının bütününün redaksiyonu backlog T80. `internal/config`'in import ettiği tek depo paketi `internal/policy` | `config.go:66-73`, `:22` |
| B23 | R7'nin tetik listesi bir `slog`/`log`/`fmt` çağrısının **metninde** `password`, `secret`, `token` … arar (büyük/küçük harf duyarsız) — literal da tanımlayıcı da o metnin parçasıdır. Emsal: operatör anahtarının env adı bir sabittedir (`config.go:452-457`) ve hiçbir `fmt` çağrısına girmez; `key32`'ye verilir, oradaki `fmt.Errorf` yalnız `name` parametresini görür (`:681-694`, çağrı `:525`) | `scripts/redline-check.sh:1459-1461` |
| B24 | `crypto/hmac` import eden üretim dosyası `internal/sun` **dışında 12**: token ve kod özetleri ile oturum anahtarından türetme (`adminauth`, `invite`, `session`, `operatorauth/token.go`), HMAC imzalı durum çerezleri (dört handler dosyası: `deactivateconfirm`, `logincontext`, `signupstate`, `tapcontext`), operatörün challenge ve enrollment zarfları, TOTP (`operatorauth/totp.go`). ADR 0020'nin *"Kriptografinin yeri"* kararı (2026-09-26, *"CLAUDE.md DEĞİŞMEZ"*): §3'ün *"Kriptografi SADECE burada"* kuralı fiilen AES / blok şifre içindir, HMAC dışarıdadır | `rg -l '"crypto/hmac"' --glob '*.go' --glob '!*_test.go' internal cmd web`; `rg -n 'hmac\.New' <dosya>`; ADR 0020 satır 102-120 |
| B25 | `go.mod` bugün **10** modül gerektirir (5 doğrudan + 5 dolaylı) | `go.mod` |
| B26 | Kodda Q02'yi anan satır: **17 dosyada 27 satır** (ikisi uygulanmış migration yorumu) | `rg -c 'Q02' --glob '!docs/**' --glob '!.claude/**' .` |
| B27 | Varsayılan tarayıcı tuzağı ölçülmüş: NFC NDEF URL'sini **varsayılan** tarayıcı açar; aktivasyon başka tarayıcıda yapılınca tap oturumsuz kalır (`/t → 303 /activate`) | [state.md](../plan/state.md) → *"Uçtan uca pilot senaryosu"* paragrafı |
| B28 | Yönetici yaratan **tek** üretim yolu kayıt sihirbazıdır ve `role = 'owner'` yazar; `manager` üründe ulaşılamaz (EM-12'ye dek) | `internal/domain/signup/signup.go:723-731`; `rg -n 'CreateAdminUser\(' --glob '!*_test.go' internal cmd` → tek üretim çağrısı |
| B29 | `employee.added` audit detail'i adresi değil yalnız `has_email` boolean'ını taşır (gerekçe yorumda: `audit_log` değişmez, GDPR silmesi ona ulaşmaz) | `internal/domain/tenant/staff.go:362-376`, `:484` |
| B30 | Panel istekleri chi `middleware.Recoverer` altında koşar; istek dışı bir goroutine onun kapsamında değildir. `internal/encode` emsali paniği yakalar, temizlik yapar ve **yeniden fırlatır** — çünkü bir istek goroutine'indedir | `internal/httpx/router.go:82`; `internal/encode/session.go:1850-1870` |
| B31 | `textproto`'nun satır okuyucusu **sınırsızdır**: `ReadLine` → `readLineSlice(-1)`; `ReadResponse` çok satırlı yanıtın satırlarını sınırsız bir `strings.Builder`'a ekler. `net/smtp` selamlamayı, EHLO yanıtını ve 250'yi bununla okur | Go 1.27.1 `net/textproto/reader.go:43-45`, `:60-80`, `:287-300` |
| B32 | `tls.Config.RootCAs == nil` sistem köklerini kullanır; Linux'ta konumlarını `SSL_CERT_FILE` ve `SSL_CERT_DIR` env'leri değiştirir (Go 1.27'den beri macOS/Windows'ta da platform doğrulamasını devre dışı bırakır) | Go 1.27.1 `crypto/x509/root.go:124-138`; `crypto/x509/cert_pool.go:106-115` |
| B33 | **Denetçinin ölçümü (EM-1 3. tur, 2026-10-03; bu turda yeniden koşturulmadı):** 2. turun *"birebir alt dizge"* yankı kuralı `250 Ok x<token>` ve `250 Ok <token>-000000` yanıtlarındaki id'yi **geçirdi** — token `Text`/`HTML`'in içindedir, ayrı bir değer değildir | üçüncü göz + güvenlik yeniden denetimi |
| B34 | **Denetçinin ölçümü (EM-1 3. tur, 2026-10-03; bu turda yeniden koşturulmadı):** boşaltma süresi D = 1 s, **uçuşta istek yokken** doğru uygulama, işlev içi `Sleep(D)` ve tamamen ardışık uygulama üçü de ≈1,0 s (Shutdown ≈0 s sürer, 0 + D = max(0, D)); `RegisterOnShutdown` → `go f()` örneğinde *"boşaltma Shutdown dönmeden başladı"* assert'i 2000 denemenin **1896**'sında kırmızı; **T = D = 1 s süren bir istek uçuştayken** doğru uygulama 1,056 s, işlev içi mutasyon 2,089 s, tamamen ardışık 2,141 s | üçüncü göz |
| B35 | `Shutdown` boşta bağlantıları artan aralıklarla yoklar; aralık en çok `shutdownPollIntervalMax` = **500 ms**, her aralığa **%10'a kadar** sapma eklenir — yani uçuştaki istek bittikten sonra Shutdown'ın dönmesi ≈550 ms'ye kadar gecikebilir | Go 1.27.1 `net/http/server.go:3220-3227`, `:3262-3272` |
| B36 | Seed'de `@kebabfactory.mt` ve `@kebabmfg.mt` alan adlarında **37** farklı adres var (bu alan adlarının posta kabul edip etmediği ölçülmedi). `.env` depoda yok sayılan bir dosyadır (`.gitignore:7`), dolayısıyla R7d'nin dosya listesine (izlenen + yok sayılmayan dosyalar) girmez | seed: `grep -ohE` ile `@kebab(factory\|mfg)\.mt` adresleri, `sort -u \| wc -l` → 37; R7d listesi `scripts/redline-check.sh:1834` |
| B37 | **Denetçinin ölçümü (EM-1 4. tur, 2026-10-03; bu turda yeniden koşturulmadı):** 3. turun §6.6(b) tarifiyle — T = 1,2 s, D = 0,9 s, pay 500 ms, eşik T + D − pay = 1,6 s — **doğru** uygulama 12 koşunun **5**'inde eşiği aştı: B35'in yoklama adımı doğru uygulamayı yavaşlatır, pay onu kapsamaz | üçüncü göz |
| B38 | `bufio.Reader.ReadLine`, okuma bir hatayla kesildiğinde elde biriken **kısmi satırı hatasız** döndürür (hata bir sonraki çağrıya kalır); `textproto` satırı buradan alır, yani `250 Ok <kesik id>` bir `250` olarak ayrıştırılır | Go 1.27.1 `bufio/bufio.go:405-428` |

### Ölçülen standart kütüphane davranışı

Sonda: yalnız stdlib, scratchpad'de ayrı modül; **go1.27.1 ve go1.26.6, darwin/amd64**. İki araç
zincirinde S2–S9 ve S12–S13'ün çıktısı **birebir aynı** (sürüm satırı dışında `diff` boş);
S14'te sonuç aynı, ayırma miktarı farklı (satırda ikisi de yazılı).
`dosya:satır` atıfları go1.27.1 kaynağındandır. Bütün değerler sentetiktir (`example.test`,
`example.net`).

| # | Girdi | Ölçülen |
|---|---|---|
| S1 | `net/smtp` paket belgesi | *"The smtp package is frozen and is not accepting new features."* (`net/smtp/smtp.go:14`) |
| S2 | `smtp.SendMail`, STARTTLS ilan etmeyen sunucu, `auth = nil` | STARTTLS yalnız ilan edilirse denenir (`smtp.go:338-345`). Sonda: `SendMail` **nil** döndü; sunucu 0 `STARTTLS`, 1 `DATA` gördü ve link satırı **düz metinde** geldi |
| S3 | `smtp.PlainAuth`, TLS olmayan bağlantı | `plainAuth.Start` TLS yoksa reddeder **ama** sunucu adı `localhost`, `127.0.0.1` ya da `::1` ise reddetmez (`auth.go:57-69`). Sonda: ad `mail.example.test` → `unencrypted connection`, sunucu **0** `AUTH` gördü; ad `localhost` → hata yok, sunucu **1** `AUTH` gördü (kimlik bilgisi düz metinde) |
| S4 | `Client.Data().Close()` | 250 yanıtını okur ve **metnini atar** (`smtp.go:281-285`); `SendMail` yalnız `error` döndürür. `Client.Text` dışa açıktır (`smtp.go:33-35`) → DATA elle sürülürse 250 metni okunabilir |
| S5 | `smtp.Dial` | `net.Dial`, süre sınırı yok (`smtp.go:53-60`) |
| S6 | `textproto.Error` | `Error()` = `fmt.Sprintf("%03d %q", Code, Msg)` (`net/textproto/textproto.go:43-45`). Sonda: SES biçimli sahte bir 554 (`… failed the check in region EU-CENTRAL-1: probe@example.net ?t=SYNTHETICPROBE`) `fmt.Errorf("invite: deliver %s: %w", …)` ile sarıldı → `Error()` adresi ve `?t=` değerini taşıdı; `errors.As(…, *textproto.Error)` **true** |
| S7 | `validateLine` | zarf adreslerinde CR/LF reddeder (`smtp.go:425-430`); **başlıklar** bizim üreticimizindir |
| S8 | `mail.ParseAddress` | `ali@example.com` → Name `""`, Address = girdi · `Ali <ali@example.com>` → Name `"Ali"` · CRLF + `Bcc:` eki → hata · iki adres → hata · `ali+payroll@…` → kabul, Address = girdi · `"ali veli"@…` → Address ≠ girdi · `ćali@…` ve `ali@exämple.com` → **kabul** (ASCII kuralı bizim) · `" ali@example.com "` → kırpılır, Address ≠ girdi · `ali@[192.0.2.1]` → kabul, Address = girdi · `ALI@EXAMPLE.COM` → kabul |
| S9 | `mime.QEncoding.Encode("utf-8", …)` | ASCII girdi **değişmeden** döner (`"Your Taptime invitation"` → aynısı); Maltaca `Ħal Għaxaq ċ ġ ħ ż` → `=?utf-8?q?=C4=A6al_G=C4=A7axaq_=C4=8B_=C4=A1_=C4=A7_=C5=BC?=` |
| S10 | `crypto/tls` `MinVersion` belgesi | *"By default, TLS 1.2 is currently used as the minimum"* (`crypto/tls/common.go:797`) — "currently" |
| S11 | `aws-sdk-go-v2` modül sayısı (scratchpad modülü, `proxy.golang.org`, 2026-10-03) | yalnız `service/sesv2` + `aws` → **6** modül (2 doğrudan + 4 dolaylı: `internal/configsources`, `internal/endpoints/v2`, `internal/v4a`, `smithy-go`); `config` eklenince **15** (2 doğrudan + 13 dolaylı; ek olarak `config`, `credentials`, `feature/ec2/imds`, `service/internal/accept-encoding`, `service/internal/presigned-url`, `service/signin`, `service/sso`, `service/ssooidc`, `service/sts`). Komut: `go mod tidy` + `go list -m -f '{{if not .Main}}{{.Path}}{{end}}' all` |
| S12 | TLS istemcisi, `MinVersion` **verilmemiş** | sunucu en çok TLS 1.1 → el sıkışma başarısız (`remote error: tls: protocol version not supported`); sunucu TLS 1.2 → başarılı. Sebep: `MinVersion == 0` iken 1.2 altı sürümler listeden düşer (`crypto/tls/common.go:1239`) |
| S13 | `mime/quotedprintable` yazıcısı, satır sonları | `"a\r\nb"` → 1 CRLF · `"a\rX\nb"` → 2 CRLF · `"a\n\x1d\nb"` → 2 CRLF · **`"a\r\x1d\nb"` → `"a\r\n=1Db"`, 1 CRLF** — CR, kodlanan bir bayt ve LF dizisinde ikinci satır sonu **kaybolur** (EM-2'nin fuzz testi de buldu) |
| S14 | `smtp.NewClient`, TLS öncesi tek satırlık dev bir `220` selamlaması (`net.Pipe`) | 1 MiB → hata yok, `TotalAlloc` +7,0 MiB · **32 MiB → hata yok, +287,8 MiB** (go1.27.1) / +255,8 MiB (go1.26.6). Okuma sınırsız ve kabaca 8–9 kat büyüyor (B31) |

## Karar

### 1. Sağlayıcı ve arayüz — Q02'nin cevabı

İşlemsel e-posta **AWS SES**, bölge **`eu-central-1`** (K7), **SMTP arayüzü** üzerinden,
**port 587 + STARTTLS** ile gönderilir; istemci stdlib `net/smtp` + `net/mail` +
`mime`/`mime/multipart`/`mime/quotedprintable` + `crypto/tls`'tir (K1). Gönderen sabit
`Taptime <no-reply@taptime.mt>` (K5; tenant adı başlığa girmez — ADR 0023 §8, WL-11), MAIL
FROM alan adı `mail.taptime.mt` (K6). Sağlayıcıya özgü her şey **yapılandırma değeri** ve
kullanıcının dış adımlarıdır ([m10](../plan/m10-platform.md) §4 *"Kullanıcının dış
adımları"*); kod genel SMTP'dir, SES'e özgü API, başlık ya da SDK kullanmaz.

Q02'nin üç kalemi: **davet linki** → §7 · **şifre sıfırlama** → §6 · **rapor gönderimi** →
kapsam dışı (§13). *"AB bölgesi ve GDPR işleme sözleşmesi"*: bölge `eu-central-1`; AWS'nin
alt işleyici olarak gizlilik politikasına ve DPA listesine eklenmesi kullanıcının 12. dış
adımıdır — AWS hesabındaki işleme sözleşmesinin geçerliliği bu ADR'de **ölçülmedi**.

### 2. `internal/mail` — taşıyıcı (→ EM-2)

- **Bağımlılık:** yalnız stdlib; depo içinden **hiçbir paketi** import etmez ve davet/sıfırlama
  kavramını bilmez. (İkinci yarı §5'in ön koşuludur: `internal/config` bu paketi import
  edebilsin.)
- **API:** `Message{To, Subject, Text, HTML, Ref}`, `Receipt{MessageID}`,
  `SMTP.Send(ctx, Message) (Receipt, error)`, `New(Config) (*SMTP, error)`.
  - **`Ref`** isteğe bağlıdır, 1–128 bayt `[A-Za-z0-9._:-]`; yalnız çağıranın log satırı
    içindir ve **iletilmez** — ne iletiye ne sunucuya (EM-2 sapması g). Kümede `@`, `/`, `?`,
    `=` ve boşluk yoktur, yani bir Ref adres ya da URL olamaz.
  - **`Message` üçüncü taşıyıcıdır ve kendi yazdırma yolları redakte eder** (eşitleme turu,
    EM-2'nin 2. tur kodu okundu): `Text` ve `HTML` linki (davet kodu, sıfırlama token'ı), `To`
    adresi düz `string` alanlarda taşır; `Message`'ın `Format` (her fiil, `%#v` dahil),
    `String`, `GoString`, `LogValue` ve `MarshalText` yöntemleri sabit bir yer tutucu basar,
    beşi de derleme zamanı doğrulamasıyla bağlı; belge yorumunda ⚠️ *"loglanmaz"* uyarısı ve
    B13'teki iki yapıyla aynı **beyan edilmiş istisna** cümlesi var (aynı üç yükümlülük:
    loglanmaz, kalıcı saklanmaz, tek alıcıya gider). **İstisnanın kalan kısmı, ölçülmüş:**
    çağıranın yapısının **dışa kapalı** bir alanında tutulan bir `Message`'ı `fmt` yansımayla
    basar, alanları dahil (`TestMessage_KnownLimitIsAnUnexportedField`); ve `m.Text` ya da
    `m.To`'yu doğrudan loglayan bir çağıran onları
    loglar. İkisi de §10'un kapalı kümesinin dışındadır ve kod incelemesinin konusudur
    (sayılı sınır 20).
- **Bayt tavanı (EM-2 sapması j, orkestratör kuralı):** ham bağlantı `smtp.NewClient`'ten
  **önce**, sunucudan okunan baytı **deneme başına** sayan bir sarmalayıcıya sarılır. Tavan
  **256 KiB**'tır; TLS öncesi ve sonrası **tek sayaçla** sayılır (sayaç ham bağlantıdadır, TLS
  sonrası şifreli baytları sayar). Aşılırsa deneme `network` sınıfıyla biter — **istisna (4. tur,
  güvenlik D1):** veri sonu yanıtında **`250 ` kodu okunduysa** satırın sonradan kesilmesi
  (tavan, süre dolması ya da iptal) **kabuldür**: gönderim başarılıdır, `message_id` boş döner.
  Gerekçe: röle 250'yi göndermiştir; `network` ya da `timeout` saymak sayılı sınır 15'in yönünü
  (gitmiş link, `undelivered` kaydı) üretir. Kök neden: `bufio.Reader.ReadLine` kısmi bir satırı
  hatasız döndürür (B38) ve QUIT'in yanıtı beklenmez, yani kesilme 250'den sonra başka bir
  okumada görünmez. Sayılı sınır 34. Gerekçe ölçüldü:
  `textproto`'nun satır okuyucusu sınırsızdır (B31) ve 32 MiB'lık tek bir selamlama satırı
  `smtp.NewClient`'te hatasız kabul edilip ≈256–288 MiB ayırttı (S14); güvenlik denetçisinin
  ölçümünde ~120 MiB'lık bir selamlama 512Mi'lik tek replikayı OOM'a soktu. Süre (§2'nin iki
  yolu) zamanı sınırlar, belleği sınırlamaz. → İddia J.
  - **Sayaç TLS el sıkışmasını da sayar:** TLS kayıtları ham bağlantıdan geçtikçe sayılır.
  - **Neden 256 KiB:** dürüst bir röle bir selamlama, iki EHLO yanıtı ve birkaç yanıt satırı —
    birkaç yüz bayt — artı **bir** TLS sunucu uçuşu gönderir; uçuşun büyüğü sertifika
    zinciridir (birkaç KiB; uzun bir RSA-4096 zinciri 16 KiB'ın altında). Tavan meşru bir
    denemenin okuduğunun 10 katından fazladır ve düşmanca bir rölenin `Send`'e tutturabileceğini
    sınırlar. `textproto.ReadResponse` çok satırlı yanıtı bir `strings.Builder` ile kurar
    (B31), yani tavanın altında karesel büyüme yoktur; EM-2'nin ölçümünde tavanın 16 katı sel
    gönderen bir rölede gönderim **0,9–2,1 MiB** ayırdı (beş vaka; bütçe tavanın 16 katı,
    4 MiB).
  - **Gönderim başına üst sınır:** yalnız 4xx yeniden denenir (aşağıda), bayt tavanının `network`
    sonucu denenmez; bu yüzden bir `Send` sunucudan en çok 2 × 256 KiB okur (önce 4xx, sonra
    tavan).
- **Sıra:** `net.Dialer.DialContext` → bayt sayacı → `smtp.NewClient` → EHLO → **STARTTLS zorunlu**:
  ilan edilmezse `SendError{Class: tls_unavailable}` ve **AUTH gönderilmez**; düz metne düşüş
  yok. Bu kontrol stdlib'in `PlainAuth` korumasına **bırakılmaz**: o koruma sunucu adı
  `localhost` iken düz metinde kimlik gönderir (S3). `tls.Config{ServerName: host,
  MinVersion: tls.VersionTLS12}` — açıkça yazılır, çünkü belge varsayılanı "currently" diye
  anlatır (S10; bugünkü varsayılan aynı sonucu verir — S12); `InsecureSkipVerify` yok → AUTH
  PLAIN → MAIL FROM (sabit) → RCPT (tek) → **DATA elle** (`Client.Text` + `DotWriter`,
  ardından `ReadResponse(250)`; S4) → `Receipt.MessageID` 250 metninden → QUIT yazılır ama
  **221 yanıtı beklenmez** (EM-2 sapması e: 250'den sonra ileti kabul edilmiştir; QUIT'e
  cevap vermeyen bir sunucu teslim edilmiş bir gönderimi süre dolana dek tutamaz).
- **Süre iki yoldan** uygulanır (EM-2 sapması i): bağlantıya `SetDeadline` ve ctx bitince
  süreyi geçmişe çeken `context.AfterFunc`. İkincisi iptali de yakalar; birincisi tek başına
  **pinli değildir** (sayılı sınır 16).
- **Kök sertifika havuzu:** `Config.RootCAs` yalnız **testlerde** dolar (öz-imzalı sahte
  sunucu); `nil` sistem kökleri demektir. **Prod'da özel kök havuzu YASAKTIR** (EM-K9'un
  güvenlik notu): yapılandırmada bir CA dosyası ya da kök anahtarı **yoktur**, `cmd/tappa`
  `mail.Config`'i `RootCAs` vermeden kurar. Gerekçe: özel bir kökü denetleyen taraf SMTP
  bağlantısının ortasına girip SMTP kimliğini ve her linki okur. → EM-3 bunu pinlere bağlar
  (tarif): (a) `internal/mail` dışındaki test-dışı Go dosyalarında **`mail.Config`'in**
  `RootCAs` alanına atama 0 — hem `c.RootCAs = …` biçimi hem anahtarlı bileşik değer
  `mail.Config{RootCAs: …}` taranır, başka `tls.Config` değerleri bu pinin konusu değildir;
  (b) config'te CA anahtarı 0; (c) hizmet konteynerinin manifestinde `SSL_CERT_FILE` /
  `SSL_CERT_DIR` env'i 0. **(c)'nin gerekçesi:** `RootCAs: nil` Linux'ta sistem köklerini
  okur ve `SSL_CERT_FILE` / `SSL_CERT_DIR` bu konumları değiştirir (B32) — manifestte bir env
  ya da `/etc/ssl/certs` üzerine bağlanan bir volume, yasağı **hiçbir Go ataması olmadan**
  aşar. Volume yolu (c)'nin dışındadır (İddia I, *Yakalamadığı*).
- **`smtp.SendMail` kullanılmaz** (S2 düz metne düşer, S4 Message-ID'yi atar, S5 süre yok).
- **Yeniden deneme:** **4xx** yanıt — AUTH'a verilen 4xx dahil (RFC 4954 §6: 454 geçici kimlik
  doğrulama hatası; EM-2 sapması b) — için bellekte, **yeni bir bağlantıda**, süre içinde
  **tek** geri çekilmeli deneme; ikinci 4xx sonuçtur. **Başka hiçbir sınıf yeniden denenmez** —
  `network` dahil: veri sonu noktasından sonra kopan bir bağlantıda 250 kaybolmuş olabilir ve
  yeniden deneme iletiyi **iki kez** teslim edebilir (`TestSend_ANetworkFailureIsNotRetried`:
  noktadan sonra düşen ve DATA ortasında sıfırlanan bağlantı, ikisi de tek bağlantıyla
  `network`). Bağlantı sayısı adım adım pinlidir: `TestSend_ClassifiesEachStep` her satırda
  sayıyı assert eder — 4xx'te **2**; selamlama 554, AUTH 535, MAIL 550, RCPT 550, DATA 554 ve
  veri sonu 554'te **1**; bağlam baştan bitmişse **0**. Orkestratörün ölçümü: `auth` ya da
  `rejected` sınıfı yeniden denenince test kırmızı. Link hiçbir kalıcı kuyruğa,
  tabloya ya da dosyaya yazılmaz (B13).
- **Gövde:** `multipart/alternative` (önce `text/plain`, sonra `text/html`), `charset=utf-8`,
  `Content-Transfer-Encoding: quoted-printable`. Satır sonları kodlamadan **önce** LF'ye
  normalize edilir — S13'ün ölçtüğü satır sonu kaybı yüzünden (EM-2'nin `alternative`'i).
  `Date` ve **`Message-ID` başlıklarını biz yazarız** (`<rastgele@gönderen-alanı>`; EM-2
  sapması d). SES'in bu başlığa ne yaptığı ve 250 yanıtının SES message-id'sini taşıması
  **ölçülmedi** — EM-5'in canlı dumanı ölçer.
- **`Receipt.MessageID` kuralı** (sunucu metninden gelen tek dize log'a gidebildiği için):
  250 yanıtının son satırının, isteğe bağlı RFC 3463 durum kodundan sonraki son alanı; en az bir
  alan ondan önce gelmeli. Şekil **1–128 bayt `[A-Za-z0-9._-]`** (`@`, `/`, `?`, `:`, `=`,
  boşluk yok); **yankı kuralı — paylaşılan pencere (EM-2 sapması k, orkestratör kuralı):**
  id ile gönderilen ya da kimlik doğrulamada kullanılan değerlerden **herhangi biri** arasında
  **k = 8** karakterlik ortak bir `[A-Za-z0-9._-]` penceresi varsa id düşürülür (`""`); **8
  karakterden kısa** bir id, gönderilen bir değerin içinde **birebir** geçiyorsa da düşer.
  **Değer listesi (EM-2'nin kodu, eşitleme turunda okundu):** tel üstündeki ham ileti, `To`,
  `From` başlığı, `Subject`, `Ref`, `Text`, `HTML`, SMTP kullanıcı adı, **parola** ve tel
  üstündeki **AUTH PLAIN argümanı** (base64). Parolanın ayrıca listede olmasının sebebi ölçüldü
  (EM-2): `x<parola>` biçimli bir id, base64'lü AUTH argümanıyla 8 karakterlik bir pencere
  paylaşmaz — parola listede olmasa bu biçim geçer. **`To` ve `From` için ölçülmüş bir
  eşdeğerlik, açık değil:** ikisi ham iletinin başlıklarında birebir geçtiği için listeden
  çıkarılmaları (EM-2'nin M39/M40 mutasyonları) testleri yeşil bırakır — ham ileti onları zaten
  kapsar; listede durmaları bu kapsamın bir kopyasıdır. Neden pencere: 2. turun
  kuralı yalnız *"id bir değerin içinde mi"* diye bakıyordu ve token ayrı bir değer değil,
  `Text`/`HTML`'in **içinde** olduğu için `250 Ok x<token>` ve `250 Ok <token>-000000` geçiyordu
  (üçüncü göz ölçtü, B33). Bedeli yanlış pozitif: meşru bir id düşebilir; sonucu yalnız
  `message_id`'nin kaybıdır (sayılı sınır 21). Kalan kaçış: sayılı sınır 17.
- **`ErrNotConfigured`** bir `*SendError` değildir: `nil` ya da `New` ile kurulmamış bir
  `SMTP`'ye `Send` → bir **programcı hatası** (EM-2 sapması c). Bunun dışındaki her hata
  `*SendError`'dır.

### 3. Sızıntı kuralı — `SendError` (→ EM-2)

- `SendError`'ın alanları **tam olarak** `Class` (string türünde) ve `SMTPCode`'dur (3 haneli
  yanıt kodu; yanıt yoksa 0); `*SendError`'ın yöntem kümesi **tam olarak** `{Error}`'dur —
  `Unwrap`, `Is`, `As`, `Format` **yoktur**. `textproto.Error` sarılmaz (S6: sarılırsa
  `errors.As` onu bulur ve `Error()` sunucu metnini taşır).
- `Error()` yalnız sınıfı ve kodu basar (`mail: <sınıf>` / `mail: <sınıf> (smtp <kod>)`);
  sunucu metni, adres, link **içermez**.
- **Kuralın neden yapısal bir pin istediği** (üçüncü göz düzeltmesi): `*SendError` bir
  `error`'dur; `%v`, `%+v` ve `%s` alanları değil `Error()`'u basar, alanları yalnız `%#v`
  gösterir. Yani `Error()`'ün basmadığı bir alan (ör. `error` ya da `[]byte` türünde) alt dizge
  arayan bir sızıntı testinde **görünmez** — ama `errors.As` ya da bir `%#v` onu dışarı çıkarır.
  Bu yüzden *"alanlar tam olarak ikisi"* kuralı **reflect tabanlı bir alan + yöntem listesi
  pinine** bağlanır: `TestSendError_HasOnlyAClassAndACode` (`internal/mail/leak_external_test.go`;
  alanlar `Class mail.Class, SMTPCode int`, `*SendError`'ın yöntemleri `Error`, değer türünün
  yöntemleri boş).
- **Sekiz sınıf** (EM-2 sapması a: `invalid_message` eklendi) ve eşleme kuralı:

  | Sınıf | Ne zaman |
  |---|---|
  | `invalid_address` | alıcı §4'ün kuralını geçemedi — **dial'dan önce** |
  | `invalid_message` | `Subject` §4'ün başlık kuralını, `Ref` §2'nin kuralını geçemedi, gövde boş ya da geçersiz UTF-8, ya da bir başlık satırı 998 sekizliyi aşıyor — **dial'dan önce** |
  | `tls_unavailable` | STARTTLS ilan edilmedi; STARTTLS'e **4xx ya da 5xx** (her yanıt kodu); başarısız TLS el sıkışması (güvenilmeyen sertifika, başka ada verilmiş sertifika, 1.2 altı sürüm, bozuk akış); **STARTTLS sonrası EHLO** hatası. Kod varsa `SMTPCode`'da; **yeniden denenmez** (454 de; `errors.go`'daki `byCode`). **Daraltma (4. tur, F4):** STARTTLS aşamasının hataları bu sınıftır **ancak** öncelik paragrafındaki iki üst kural uygulanmıyorsa — el sıkışmada bayt tavanı aşılırsa `network`, süre dolar ya da iptal edilirse `timeout` |
  | `auth` | AUTH'a **5xx** (ya da yerel PLAIN reddi) |
  | `rejected` | AUTH dışında 5xx (selam, MAIL, RCPT, DATA, veri sonu) |
  | `throttled` | STARTTLS dışında **4xx** (AUTH dahil). Tek deneme yapılır; ikinci 4xx de `throttled`dır. **Deneme yapılamazsa da** sınıf `throttled` kalır, `timeout` olmaz: geri çekilme süresine yetecek süre yoksa ya da bağlam geri çekilme sırasında biterse ilk 4xx'in hatası döner (EM-2'nin `mail.go`'daki `Send` + `pause`). **Gerekçe (EM-2'nin kararı, orkestratör kabul etti):** gönderimi bitiren rölenin 4xx yanıtıdır, süre yalnız yeniden denenmeyeceğine karar verdi; `timeout` sağlayıcı kısıtlamasını — çağıranın log'unda burada gereken tek işletim sinyalini — gizler ve yanıt kodunu taşımaz |
  | `network` | bağlantı hatası, protokol ihlali (beklenmeyen kod, bozuk yanıt, kapanan akış) ya da **§2'nin bayt tavanının aşılması** (EM-2'nin `classify`'ı tavanı ilk sırada `network` yapar); yeniden denenmez (§2) |
  | `timeout` | bir konuşma adımında ctx süresi doldu ya da iptal edildi (TLS el sıkışması dahil — süre, aşamadan önce bakılır). **İstisna (D1):** veri sonu yanıtında `250 ` okunduktan sonraki süre dolması ya da iptal `timeout` değildir — gönderim başarılı, `message_id` boş (§2, sayılı sınır 34) |

  **Öncelik (EM-2'nin `classify`'ı, birebir):** bayt tavanı → süre/iptal → yanıt kodu (aşamaya
  göre: STARTTLS aşamasında `tls_unavailable`, 4xx `throttled`, AUTH'ta 5xx `auth`, öteki 5xx
  `rejected`, beklenmeyen kod `network`) → kodsuz STARTTLS aşaması hatası `tls_unavailable` →
  kodsuz AUTH değişimi hatası `auth` → kalan `network`. **Pin durumu:** el sıkışmada tavan →
  `network` pinlidir (`TestSend_TheReplyCapCoversTheWholeAttempt`, el sıkışma vakası; M16
  kırmızı). **Süre ile tavanın kendi aralarındaki sırası pinsizdir** — sayılı sınır 35.

- **Gerekçe (ölçüldü):** davet hata yolu bugün zincirin tamamını logluyor (B11), sıfırlama yolu
  aynı sınıfı bir güvenlik denetiminde yaşadı (B5, B6), ve SES biçimli bir red metni hem
  adresi hem `?t=` değerini taşıyabilir (S6 sondası). Bu ADR'nin log'a izin verdiği hata
  bilgisi sınıf + koddur (§10). Sunucu metninin kaybı bilinçli bir bedeldir: tanı SES
  konsolunda ve message-id üzerinden yapılır (§11).

### 4. Başlık ve alıcı kuralları (→ EM-2)

- **Başlık değeri kuralı** (EM-2 sapması f — C0/DEL'den katı): serbest metinli bir başlık değeri
  (`Subject`, gönderen görünen adı) boş olmayan geçerli UTF-8'dir ve **C0 (TAB, CR, LF dahil),
  DEL, C1** kontrol karakteri, **U+2028/U+2029** ya da **`=?`** içerirse **reddedilir**
  (temizlenmez, kodlanmaz). `=?` gerekçesi ölçüldü (EM-2'nin fuzz testi): `mime.QEncoding`
  yazdırılabilir ASCII'yi değiştirmeden geçirir (S9), yani encoded-word **görünümlü** bir ASCII
  konu (`=?utf-8?q?a=0D=0ABcc:_b?=`) tele olduğu gibi gider ve okuyucular onu **CR LF + sahte
  bir `Bcc:` satırına** çözer. İkinci kapı: kodlanmış her başlık satırı yazdırılabilir ASCII ve
  ≤ 998 sekizli olmalı, yoksa ileti reddedilir. İkisi de bağlantıdan **önce** (dial sayısı 0).
- **Konular sabittir** ve ASCII'dir; S9'a göre kodlayıcı onları değiştirmeden geçirir. Tenant
  ve çalışan adı **yalnız gövdede**, kaçışlı (ADR 0023 §8).
- **Alıcı kuralı** (`To`): form sınırında yalnız çevreleyen boşluk kırpılır (`adminreset.go`
  emsali), sonra değer `x` için: her bayt `0x21–0x7E` (ASCII, boşluksuz) **ve**
  `len(x) ≤ 254` **ve** `mail.ParseAddress(x)` başarılı **ve** `Address == x`. Ayrı bir
  *"görünen ad boş"* kontrolü **yoktur** (EM-2 sapması h): *"ayrıştırılan adres = girdi"* onu
  kapsar — görünen ad, köşeli parantez, yorum, çevreleyen boşluk, tırnaklı yerel kısım ya da
  ikinci adres `Address`'i girdiden farklı kılar ya da ayrıştırmayı düşürür (S8). ASCII dışı
  adres bayt kuralına takılır (S8: `net/mail` onu kabul eder). Alıcıda da **`=?` reddedilir**
  (EM-2'nin 2. tur kararı): `To` başlığa olduğu gibi yazılır ve `Subject`'teki fuzz bulgusuyla
  aynı mekanizma — encoded-word görünümlü bir değerin okuyucuda çözülmesi — orada da geçerlidir.
  Köşeli parantezli alan adı (`ali@[192.0.2.1]`) kuraldan **geçer** — sayılı sınır 7.
- **`Reply-To`** verilirse `New`'de **alıcı kuralıyla** doğrulanır (görünen ad yok; EM-2'nin 2.
  tur kodu: `mail.go`'daki `New`, `validRecipient`). `From` görünen ad taşıyabilir ve başlık
  değeri kuralına tabidir.
- **Kural gönderim anında, saklanan değere** uygulanır: B14'ün saklama kuralı daha zayıftır,
  dolayısıyla saklanmış bir adres bu kuralı geçemeyebilir (ör. ASCII dışı). Davette bu durum
  kod basılmadan önce yakalanır (§7). Migration ya da geriye dönük düzeltme **yok**.

### 5. SMTP kimlik bilgisi ve yapılandırma (→ EM-3)

| Anahtar | Yer | Değer / kural |
|---|---|---|
| `TAPPA_RESET_DELIVERY` | ConfigMap | `none` \| `email`; boş = `none`; başka değer boot hatası |
| `TAPPA_INVITE_DELIVERY` | ConfigMap | `panel` \| `email`; boş = `panel`; başka değer boot hatası |
| `TAPPA_SMTP_HOST` | ConfigMap | DNS adı; **prod'da** `localhost`, `*.localhost` ve IP literal (`netip.ParseAddr` başarılı) **red** |
| `TAPPA_SMTP_PORT` | ConfigMap | varsayılan `587`; `465` (örtük TLS) **her ortamda red** — tasarım STARTTLS'tir, STARTTLS istemcisi örtük TLS portunda el sıkışmayı bekler |
| `TAPPA_SMTP_USERNAME` | Secret, `optional: true` | `mail.Credential` olarak tutulur (EM-2'nin 2. tur kodu) |
| `TAPPA_SMTP_PASSWORD` | Secret, `optional: true` | `mail.Credential` olarak tutulur (aşağıda) |
| `TAPPA_MAIL_FROM` | ConfigMap | `Taptime <no-reply@taptime.mt>`; boot'ta ayrıştırılır (§4'ün başlık kuralı görünen ada da uygulanır) |
| `TAPPA_MAIL_REPLY_TO` | ConfigMap (ops.) | verilirse §4'ün **alıcı kuralıyla** doğrulanır (görünen ad yok) |

Bir CA dosyası ya da özel kök anahtarı **yoktur** (§2, EM-K9).

- **Fail-closed:** en az bir akış `email` iken SMTP anahtarlarından biri eksik ya da geçersizse
  **boot hatası**, hata eksik değişkenin **adını** söyler. İki akış da kapalıyken (`none` +
  `panel`) SMTP anahtarları okunmaz ve doğrulanmaz: runbook'un ara durumu (Secret dolu,
  ConfigMap henüz `none`/`panel`) boot'u düşürmemeli (m10 dış adım 10).
- **R7 tuzağı (B23):** R7 bir `fmt`/`log`/`slog` çağrısının metnini okur — içindeki literal da
  tanımlayıcı da o metnin parçasıdır. Değişken adı ne literal olarak (`…PASSWORD…`) ne de
  tetik kelime taşıyan bir tanımlayıcıyla (ör. `envSMTPPassword`'ü doğrudan `fmt.Errorf`'e
  vermek) çağrıya girer; ad bir sabitte durur ve çağrıya nötr adlı bir parametreyle ulaşır —
  emsal: operatör anahtarının env sabiti `key32`'ye verilir, oradaki `fmt.Errorf` yalnız `name`
  görür (`config.go:681-694`; desen okunarak, koşturulmadı). Sabitin dizge olarak `config.go`'da
  durması paketleme testinin (B21) onu görmesi için gereklidir.
- **Kimlik tipi:** `internal/mail` bir redakte eden tip tanımlar — EM-2'de **`mail.Credential`**
  (`credential.go`; `Format`, `String`, `GoString`, `LogValue`, `MarshalText` → sabit yer
  tutucu; dolaylı `*string` alanı, dışa kapalı alandan `%+v` ile okumayı bir işaretçi adresine
  çevirir) — `invite.Code` deseni (`internal/invite/code.go:122-165`). **Parola da kullanıcı
  adı da** bu tiptedir (`mail.Config.Username` ve `.Password`, ikisi de
  `mail.NewCredential(…)`; SES'te kullanıcı adı bir AWS erişim anahtarı kimliğidir). Değerler
  **config yüklenirken** bu tipe sarılır: `internal/mail` depo paketi import etmediği için (§2) `internal/config` onu
  import edebilir (B22). EM-3 bunu `go list -deps` ile doğrular; bir döngü çıkarsa OP-7
  emsaline (config ham tutar, `cmd/tappa` tek kurulum yerinde sarar — `config.go:66-73`) döner
  ve kartına yazar. Kullanıcı adı log'a da yazılmaz (§10'un kapalı kümesinde yok).
- **Manifest:** ConfigMap'e yukarıdaki ConfigMap anahtarları **bugünkü davranışla**
  (`none`/`panel`); `20-app.yaml`'a iki `optional: true` `secretKeyRef` (B20 emsali). EM-3
  sevk edildiğinde davranış **değişmez**.
- **Bayatlayan metin:** `resetDelivery`'nin hata cümlesi ve ConfigMap yorumu Q02'yi "seçilmedi"
  diye anar (B2, B3). EM-3 cümleyi kapalı kümeye ve bu ADR'ye çevirir ve
  `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed`'un `"Q02"` beklentisini **bilerek**
  günceller.

### 6. Sıfırlama gönderimi istek yolundan çıkar (→ EM-5)

Zorunlu, çünkü senkron bir SMTP gönderimi (TLS + AUTH + DATA) 250 ms tabanı aşabilir ve
taban yalnız altında kalan işi eşitler (B7).

1. **İstek yolu:** `IssueForEmail` **senkron** kalır (token basılır, kardeşler emekliye ayrılır).
   Her grant sınırlı, süreç içi bir kuyruğa **bloklamadan** sunulur. Sunum başarısızsa (kuyruk
   dolu ya da kapanıyor) o grant için **senkron** `admin.recovery.undelivered` satırı yazılır,
   gönderim yapılmaz. Yanıt ve taban değişmez; yanıt süresi artık gönderime bağlı değildir.
2. **Tampon boyu — gerekçe:** alt sınır `ResetWindow` (8): tek bir isteğin tam pencere grant'ı
   boş bir kuyruğu taşırmamalı. Üst sınırı iki şey çeker: (a) boşaltma süresi dolduğunda
   tampondaki her grant'a bir `undelivered` satırı yazılır ve bu yazımlar boşaltma bütçesine
   sığmalı; (b) süreç ölümünde tampondaki her grant audit'siz kalır (sayılı sınır 1), yani
   tampon büyüdükçe o pencere büyür. Saldırı altında taşma bilinçli bir bozulmadır; fazlası
   gönderilmeden `undelivered` olur. Ölçek, aritmetikle: **tek** kaynak adres 10 dakikada en
   çok 160 grant sunar (B9); en kötü gönderim süresinde (15 s) tek işçi 10 dakikada 40 grant
   işler, yani 32'lik tampon tek adresle de dolabilir, ama ≥ 160'lık bir tamponu tek adres
   dolduramaz — **her boyda** tamponu ancak **birden çok kaynak adres** doldurur (3E N4). **Başlangıç değeri 32**
   (dört tam pencere); kesin değer, boşaltmadaki `undelivered` yazımlarının ölçülen süresiyle
   EM-5'te.
3. **İşçi:** 1–2 goroutine; her grant için ctx =
   `context.WithTimeout(context.WithoutCancel(istekCtx), 15 s)`; işçi mevcut `h.deliver`'ı
   çağırır — **gönder-sonra-tek-audit** sırası korunur (`adminreset.go:513-521`). Audit yazımı
   gönderimin tükettiği ctx'i **paylaşmaz**: bugün `deliver` ikisini aynı ctx'le yapar (B5),
   15 s'yi tüketen bir gönderim audit'i de düşürür — EM-5 audit'e kendi sınırını verir.
4. **İşçide panik:** işçi goroutine'i `middleware.Recoverer`'ın kapsamında **değildir** (B30).
   Her grant kendi `recover`'ıyla sarılır: o grant için **henüz audit satırı yazılmadıysa** panik
   bir `undelivered` satırına döner (satır yazıldıktan sonraki bir panik ikinci satır
   yazdırmaz); log'a sabit bir cümle + `reset_id` + `admin_user_id` yazılır — **panik değeri
   yazılmaz** (iletinin ya da linkin bir parçasını taşıyabilir) — ve işçi sonraki grant'a geçer.
   250 ile audit arasındaki bir panik linki göndermiş ama `undelivered` yazmış olur (sayılı
   sınır 15). Paniği
   **yeniden fırlatmaz**: `internal/encode` emsali yeniden fırlatır çünkü bir istek
   goroutine'inde, Recoverer'ın altında koşar; burada yeniden fırlatmak süreci öldürür ve
   tampondaki her grant'ı sayılı sınır 1'e iter.
5. **Audit sayımı:** her grant **tam bir** satırla (`requested` ya da `undelivered`) biter —
   **hesap başına audit bütçesinin içinde ve süreç ölmediği sürece.** Bütçeyi aşan grant'lar
   bugünkü kuralla tek bir `admin.recovery.rate_limited` satırına sayılır (B8); bu ADR o kuralı
   değiştirmez. Süreç ölümü sayılı sınır 1'dir.
6. **Kapanış:** sinyal geldiğinde kuyruk yeni grant almayı bırakır; boşaltma **≤ 3 s**'dir ve
   HTTP boşaltmasıyla **eşzamanlı** başlar (ör. `srv.RegisterOnShutdown` ile — B19: Shutdown
   onu kendi goroutine'inde başlatır ve **beklemez**). `cmd/tappa`, havuzu kapatmadan önce
   boşaltmanın bitmesini bekler; böylece kapanış dizisinin toplamı
   `max(httpShutdownGrace, boşaltma)` olur, toplam değil. Süre dolunca uçuştaki gönderim
   iptal edilir; uçuşta ya da tamponda kalan her grant'a gönderilmeden `undelivered` satırı
   yazılır — bu yazımlar boşaltma bütçesinin içindedir. **Pin (tarif, → EM-5; 3. turda yeniden
   yazıldı):**
   - (a) adlandırılmış bir boşaltma süresi sabiti D, `httpShutdownGrace`'e iki emsal yuvalanma
     testinin biçimiyle bağlanır.
   - (b) kapanış dizisi `main`'in içine satır satır değil, **testin çağırabildiği bir işleve**
     yazılır ve bir davranış testi onu sürer: süresi **T ≥ D** olan bir istek **uçuşta
     tutulur** (Shutdown onu bekler), boşaltma takılan bir gönderimle D'yi doldurur; assert:
     dizinin toplam süresi **< T + D − pay**. Ardından kalan grant'ların `undelivered` aldığı
     sayılır.
   - **Pay ve D'nin koşulu (4. turda düzeltildi, F2):** 3. turun gerekçesi tersti — Shutdown'ın
     yoklama adımı **doğru** uygulamayı yavaşlatır (uçuştaki istek bittikten sonra ≈550 ms'ye
     kadar, B35), yani doğru uygulama ≈ T + 550 ms'ye kadar sürebilir; denetçinin ölçümünde
     T = 1,2 s, D = 0,9 s, pay 500 ms iken doğru uygulama 12 koşunun 5'inde eşiği aştı (B37).
     Doğru uygulamanın eşiğin altında kalması için gerekli koşul:
     **D − pay > `shutdownPollIntervalMax` + %10 sapma (≈550 ms)** ve ölçüm gürültüsü kadar
     daha. Ardışık mutasyon en az T + D sürer, yani her pay > 0 onu eşiğin üstünde bırakır.
     Örnek: T = D = 1 s, pay = 300 ms → eşik 1,7 s; doğru uygulama ≤ ≈1,55 s, ardışık ≥ 2 s.
   - **Neden uçuşta istek şart:** uçuşta istek yokken Shutdown ≈0 s sürer ve 0 + D = max(0, D);
     doğru uygulama, işlev içi `Sleep(D)` ve tamamen ardışık uygulama üçü de ≈D ölçülür (B34).
     Denetçinin ölçümü (B34): T = D = 1 s'de doğru 1,056 s, işlev içi mutasyon 2,089 s, ardışık
     2,141 s — o koşularda pay 500 ms ile eşik 1,5 s üçünü ayırdı, ama D − pay = 500 ms yukarıdaki
     koşulu sağlamaz; B37 aynı şeklin kırmızı-yanlış verdiğini gösterdi.
   - **Başlama sırası:** assert edilecekse boşaltmanın başladığı an `go`'dan **önce**, eşzamanlı
     olarak kaydedilir; `go f()`'in içinde kaydedilen bir anla kurulan *"Shutdown dönmeden
     başladı"* assert'i doğru uygulamada da kırmızıdır (B34: 2000 denemenin 1896'sı). Ya da
     sıra assert'i hiç kullanılmaz — (b)'nin süre assert'i ardışıklığı zaten ayırır.
   - **Bu tarifin yakalamadıkları** (İddia F, *Yakalamadığı*): dizi işlevinin **dışında**
     `main`'e eklenen bir bekleme (B18: mevcut kapanış testleri yalnız sabit okur); süresi
     **D − pay − ≈550 ms**'den kısa ek bir bekleme (eşikle doğru uygulamanın üst sınırı
     arasındaki boşluk); yalnız testin kurmadığı bir koşulda (ör. tampon doluyken) ödenen bir
     bekleme; D'nin değerinin kendisi ((a) bağlar).
7. M7-04'ün sahteye karşı koşmuş kriterleri **gerçek SMTP'ye karşı** yeniden koşulur (M7-04
   kart notu 6'nın devri; EM-5 kabulü).

### 7. Davet akışı (→ EM-6, EM-7)

- **`TAPPA_INVITE_DELIVERY=panel`** bugünkü davranıştır (`ManagerVisibleChannel`, B10) ve
  kalır (K13).
- **`email` modunda**, `IssueAndDeliver`'dan **önce** çalışanın adresi okunur — yeni bir sqlc
  sorgusu, açık tenant filtresi + RLS, gövde için işletme adıyla (EM-6). Adres **yok**,
  §4'ün kuralını **geçmiyor**, ya da aynı tenant'taki **herhangi bir** `admin_users`
  satırının adresine `citext` olarak **eşit** ise: kod basılmaz, `employee_invites` satırı 0,
  yöneticiye net bir cümle, bir ret audit satırı.
- Aksi hâlde `IssueAndDeliver` bir e-posta kanalıyla **senkron** gönderir: davet kimliği
  doğrulanmış bir panel eylemidir, numaralandırma kehaneti yoktur ve yönetici sonucu ekranda
  görmelidir (§4.6). Ekran (anlamı; metin İngilizce): *"`<adres>` adresine gönderildi, N gün
  geçerli"* (süre satırın kendi zaman damgalarından, `expiryPhrase`). Yanıt gövdesinde
  `/activate?code=` **0**.
- **Audit:** başarı → `invite.code_emailed` (detail: `invite_id`, `employee_id`, `expires_at`,
  `channel: "email"`, `message_id`; **adres yok**); başarısızlık → bir satır (adı EM-7'de;
  `admin.recovery.undelivered` emsali) `class` ve `smtp_code` ile; `email` modunda
  `invite.code_shown_to_manager` **0**. Adresin audit'te olmaması B29'un gerekçesini izler
  (değişmez tablo, GDPR silmesi ulaşmaz); bedeli, alıcının yalnız adres düzenlenene dek
  `employees` satırında görünmesidir (sayılı sınır 2).
- **Hata yolu:** `employeeactions.go:419` zinciri loglamaya devam eder; zincirde yalnız
  `SendError` (§3) ve kimlikler bulunur. EM-7 bunu sızıntı testiyle pinler.
- **"E-postayı değiştir"** (EM-6): `employees.email`'i günceller ve bekleyen davetleri
  `CancelPendingInvitesForEmployee` ile **aynı transaction'da** iptal eder + audit (detail'de
  adres yok — satır **değişikliğin olduğunu** gösterir, **değeri** göstermez). Gerekçe: eski
  adrese gitmiş bir kod, adresle birlikte ölmeli.
- **"Shown once" yedeği** (K3): `email` modunda adresi olmayan çalışan için "linki göster"
  yalnız **owner** rolüne açıktır ve `ManagerVisibleChannel`'dan geçer → audit
  `invite.code_shown_to_manager`, detail `channel: "manager_panel"`. M6-11 iki kanalı ayrı
  gösterir (EM-8). **EM-12'ye dek owner-only kısıtı boştur** (B28: bugün her yönetici
  `owner`'dır); o güne dek bu yoldaki daralma yalnız **kanal ayrımıdır** (EM-8).
- **Bilinen artık (B12):** teslimat commit'ten sonra; başarısız gönderim çalışanı eski linki
  emekliye ayrılmış, yenisi gitmemiş hâlde bırakabilir; sonraki basış düzeltir. E-postayla
  daha sık görülebilir; sayılı sınır 11.
- **Y-D (ADR 0005 risk 5):** **daralır, kapanmaz** — ADR 0005 §5'in 2026-10-03 ek notu.

### 8. Şablonlar (→ EM-4, EM-9)

Yalnız İngilizce (K4) · UTF-8 · sabit ASCII konu (§4) · HTML'de satır içi stil, `tappa-brand`
paleti, metin wordmark *"Taptime"* · görsel, uzak yazı tipi, dış URL, izleme pikseli **yok** ·
şablonun yazdığı **tam bir** literal mutlak URL: eylem linki (EM-9'un *"parolanız değişti"*
bildiriminde: giriş sayfasının adresi, token'sız) · düz metin parçası linki kopyalanabilir taşır
· tenant ve çalışan adı yalnız gövdede, kaçışlı · davet metni linkin **telefonun ana
tarayıcısında** açılmasını söyler (B27 ölçülmüş tuzak; iOS'ta Gmail/Outlook uygulama-içi
tarayıcısının çerezi Safari ile paylaşmadığı iddiası **ölçülmedi** — EM-8).

**"Tek URL" kuralının sınırı — DKIM imzalı içerik (güvenlik ORTA, 3. tur).** Kayıt açık ve
doğrulamasızdır, tenant adı serbest metindir ve URL benzeri bir dize taşıyabilir, ve davet keyfi
adreslere gönderilebilir. Kaçışlama adın HTML olarak yorumlanmasını önler, **metin olarak
görünmesini önlemez**; e-posta istemcileri düz metindeki URL'leri çoğunlukla kendiliğinden link
yapar (yaygın davranış, **ölçülmedi**). Sonuç: Taptime'ın alan adıyla SPF/DKIM/DMARC'tan geçen,
gövdesinde saldırganın seçtiği bir metin ve tıklanabilir bir URL bulunan bir oltalama e-postası.
*"Tek mutlak URL"* yalnız şablonun yazdığı literal için doğrudur. **Tasarım kararı devredildi**
(EM-4, EM-7, WL-11): tenant adının e-postadaki biçimi (ör. URL benzeri adın reddi ya da
nötrleştirilmesi, adın yalnız doğrulanmış tenant'ta gösterilmesi) — sayılı sınır 22.

**EM-4 notu (2026-10-03, uygulama — `web/templates/email`; davranış bugün değişmez, hiçbir
handler çağırmaz; 2. tur aynı gün).** Normatif içerik değişmedi; §8'in dört açık noktası şöyle
kapandı:
- **Ad kapısı (sayılı sınır 22'nin EM-4 yarısı) — kaçışlamanın üstüne bir izin listesi, ad
  temizlenmez, gizlenir.** Tenant ya da çalışan adı gövdede yalnız her rune'u harf, birleşen
  işaret, ondalık rakam, U+0020 boşluk ya da `& ' ’ - – — , ( ) !` ise, en az bir harf varsa ve
  her `.` adın sonundaysa ya da ardından boşluk, `,` veya `)` geliyorsa gösterilir (`Ltd.`,
  `Co., Ltd.`, `(Malta Ltd.)` geçer; `evil.example` geçmez). Basılabilir ASCII'nin tamamı
  sayılır (3. tur): iki harf arasında yalnız harf, rakam ve `& ' - , ( ) !` gösterilir
  (`TestNames_EveryASCIICharacterBetweenTwoLetters`), `.`'dan sonra yalnız boşluk, `)` ve `,`
  (`TestNames_TheDotRule`, 0x20–0x7E taraması + tablo); geçmeyen adın yerine nötr sözcükler (*"Hello,"*, *"Your employer"*)
  durur ve e-posta yine gider. Gerekçe: testlerin dedektörünün saydığı her adres biçimi
  listenin dışında bir karakter ister (`:`, `/`, `@`, iki etiket arasında nokta) ve `.` dışında
  NFKC biçimi `.` ya da U+3002 taşıyan **34** kod noktasının (Unicode 16.0, Python
  `unicodedata` ile ölçüldü; Python'un `idna` codec'i (IDNA 2003) `evil<c>example`'ı U+3002,
  U+FF0E, U+FF61, U+FE52 ve U+2024 için `evil.example`'a çevirir — ölçüldü) hiçbiri harf, işaret
  ya da ondalık rakam değildir (34'ü testte, testi koşan Go'nun `unicode` tablolarıyla yeniden
  ölçülür — go1.27.1'de Unicode 17.0, go1.26.7'de de yeşil). Kötü biçimleri saymak yerine izin
  listesi: CLAUDE.md §5'in adres aralığı geçmişi. **Ölçülen küme (gizlenir):** testin listelediği
  düşmanca adlar — URL, çıplak ve noktalı alan adı, 34 NFKC noktası, noktaya benzeyen üç
  noktalama (U+00B7, U+30FB, U+2027), `@`'li adres, IP, `javascript:`, işaretleme, bidi, sıfır
  genişlikli boşluk, CR/LF/U+2028/U+2029/NEL/TAB, harfsiz ad — ve gövdenin görünen metninde
  linklenebilir dizi **0** (`TestNames_AnAddressShapedNameIsWithheld`; dedektörün kendi
  kontrolü `TestLinkifiable_CatchesWhatItExistsToCatch`). Bedel ölçüldü: seed'deki 2 tenant ve
  **36** çalışan adının **0**'ı gizlenir — okuyucu her satırdan tam bir ad okur ve satır sayısı
  ikinci, bağımsız bir çapayla sayılır (1. turda okuyucu virgülden sonra boşluk istiyor ve
  boşluksuz yazılmış 3 satırı sessizce atlıyordu; denetçi 33/36 ölçtü) —
  `TestNames_AnOrdinaryNameIsShownVerbatim`; alışılmadık ama meşru adlar (`J.B. Bar`,
  `Fish/Chips`, `Wine+Dine`) gizlenir (`TestNames_KnownLimitIsAnUnusualNameWithheld`);
  satır sonu taşıyan ad düz metne satır eklemez (`TestNames_ALineBreakNeverReachesTheTextPart`).
  **Kalan, sayılı:** istemcilerin neyi linklediği **ölçülmedi** (dedektör bizimdir ve bilerek
  geniştir); **dedektörün etiket ayracı olmayan, noktaya benzeyen her karakter (harf, işaret,
  rakam) gösterilir** — ölçülen örnekler U+A4F8 (Lm), U+0323 (Mn), U+0660 ve U+06F0 (Nd):
  `evil<c>example` insana adres gibi okunur; rakamlar (telefon numarası; bir istemcinin veri
  dedektörü ölçülmedi), tam genişlikli rakamlar ve düz saldırgan düzyazısı (*"Your account is
  suspended Call 21234567 now!"*) gösterilir — `TestNames_KnownLimitIsALookalikeDotAndPlainProse`
  ile ölçülmüş sınır. **Bu kalan, EM-7'nin önünde bir ürün kararıdır:** EM-7 `email` modunu
  açmadan önce kullanıcı, ya adın yalnız doğrulanmış tenant'ta gösterilmesini ya da DKIM imzalı
  davette saldırganın seçtiği düzyazının ve rakamların görünme riskinin açıkça kabulünü
  seçer (sayılı sınır 22).
- **URL sayımı — parçalar ayrı sayılır, her biri tam 1.** HTML'de tek mutlak URL tek `href`'tir
  (görünen metinde URL yok); düz metinde link kendi satırında bir kez. Link, `BaseURL`
  (sondaki `/`'ler `internal/invite` ve sıfırlama handler'ı gibi kırpılır) + yol + `?<param>=` +
  1–128 `[A-Za-z0-9_-]` biçiminde olmak zorundadır; değilse `ErrLink`/`ErrBaseURL` ve boş
  `Message`. Taban: yalnız `https` — **düz `http` yalnız loopback'te** (`localhost`,
  `*.localhost`, 127.0.0.0/8; güvenlik denetimi kararı, 2. tur: başka host'ta kod ya da token
  açık metin gider — `127.` ile başlayan ad, noktasız `…localhost` ve özel 10/8, 192.168/16
  adresleri red, 3. tur); host için bir **sözdizimi** kuralı: etiketler `[A-Za-z0-9-]`, 1–63
  bayt, kenarda `-` yok, toplam ≤ 253 bayt (`https://:443`, `https://-`, `-app.`, 64 baytlık
  etiket, 254 baytlık host red); **IPv4 biçimi doğrulanmaz** — `https://999.999.999.999` ve
  `https://1.2.3` geçer (ölçülmüş bilinen sınır); port 1–65535; IPv6 literal bayt kümesi
  dışında. Ad kapısı sayesinde **ölçülen ad kümesinde
  gövdenin görünen metninde linklenebilir başka dizi 0** (dedektörümüzle) — yani §8'in
  *"yalnız şablon literali"* çekincesi o kümede sayıyı değiştirmez. Testler
  `TestRender_EachPartCarriesExactlyOneAbsoluteURL`,
  `TestRender_RefusesALinkOutsideTheExpectedAddress`, `TestRender_AcceptsTheResetLinkAdminauthMints`.
- **Sıfırlama e-postası kimseyi adlandırmaz.** `ResetDelivery` bugün ad taşımaz ve adın
  eklenmesi, herkesin her adres için tetikleyebildiği bir e-postaya kayıtta seçilmiş metni
  sokar. Bedeli: birden çok yönetici hesabına çözülen bir adres hesap başına bir, birbirine
  benzeyen e-posta alır (en çok `MaxCandidates` = 8) — EM-5'in kararı; ad eklenirse kural ad
  kapısıdır.
- **Düz metin üreticisi düz Go'dur** (`text/template` değil, templ değil): templ HTML için
  kaçışlar (`&` → `&amp;`), düz metnin kaçışlanacak sözdizimi yoktur; tek kontrol yukarı akıştadır
  (ad kapısı denetim karakteri, satır sonu, U+2028, bidi geçersiz kılma kabul etmez; link değeri
  yalnız base64url). İki parça **aynı** cümle dizilerinden basılır
  (`TestRender_TheTwoPartsSayTheSameWords`). Konu sabit ASCII'dir, `mime.QEncoding`'den değişmeden
  geçer ve bütün ileti `internal/mail`'in derleyicisinden geçer
  (`TestSubject_IsFixedASCIIAndPassesTheMailComposer`). Davet metni linkin telefonun **ana
  tarayıcısında** — *"the one that opens when you tap a link"* — açılmasını söyler, *"own
  browser"* demez (varsayılanı Safari olmayan bir iPhone'da "own" Safari diye okunabilir) —
  `TestInvitation_SaysToUseThePhonesMainBrowser`. Kontrast her metin düğümü için satır içi
  stillerden hesaplanır ve renk taşıyan her bildirim (`color`, `*color*`, `background*`,
  `border*`, `outline*`, `text-decoration*`, `column-rule*`, `-webkit-text-stroke*`,
  `text-emphasis*`, gölgeler, `fill`, `stroke`) yalnız palet hex'i, uzunluk ve çizgi biçimi
  içerir; `opacity`, `filter`, `backdrop-filter` ve karışım kipleri hiç bildirilmez; her
  `#`-dizisi tam altı hanedir (1. turdaki tarama `red`'i ve 8 haneli `#C9D2C880`'ı, 2. turunki
  düğmenin kendi `text-decoration`'ındaki `red`'i ve `filter`'ı görmüyordu — denetçi ölçtü)
  — `TestContrast_EveryTextOnItsGroundClearsAA`: ink/paper 16,17:1 · ink/porcelain 14,32:1 ·
  paper/tappa-green 7,73:1 · tappa-green/porcelain 6,85:1; istemcilerin karanlık modu
  **ölçülmedi**. Wordmark ürün kilidi gibi küçük harf *"taptime"*, düzyazıda *"Taptime"*;
  wordmark, başlık ve düğme Space Grotesk'i yerel yazı tipi olarak ilk sırada adlandırır
  (`TestRender_DisplayFaceOnWordmarkHeadingAndButton`).

### 9. Oran sınırları (→ EM-5, EM-7)

- **Davet:** tenant başına saatte 50 + günde 300; çalışan başına saatte 3 (m10'un başlangıç
  sayıları). Sınırda red, kod basılmaz.
- **Süreç geneli devre kesici:** saatte ~300 gönderim; **her** gönderimi sayar (davet,
  sıfırlama, bildirim).
- **Sayıların savunması uygulama görevindedir**; bu ADR'nin ölçtüğü girdiler:
  - **Davet yolu:** kayıt kaynak adres başına saatte 3 tenant ve **ömür boyu tavan yok** (B17).
    İlk saatte tek adres 3 tenant × 50 = 150 davet gönderebilir; tenant'lar birikir, ikinci
    saatte 6 × 50 = 300 → **tek adres ikinci saatte devre kesiciye ulaşır**.
  - **Sıfırlama yolu:** kaynak adres başına 10 dakikada en çok 20 × 8 = **160** gönderim (B9),
    saatte 960 — tek başına kesicinin üstünde.
  - İki yol da sayılı sınır 6'dadır (paylaşılan kesici ve SES hesap itibarı).

### 10. Log kuralı (→ EM-2, EM-5, EM-7)

Bir gönderimle ilgili log satırının anahtarları **kapalı kümedir**: `tenant_id`,
`employee_id` ya da `admin_user_id`, `invite_id` ya da `reset_id`, `message_id` (§2'nin
kuralından geçmiş), `class`, `smtp_code`, ve sıfırlama isteği yolunda bugün de yazılan `ip`
(`adminreset.go:574`). Bugünkü iki satırın taşıdığı iki anahtar da kümededir, **değerleri
sınırlı** olarak (3E N1):
- `err_type` — sıfırlama yolunun bugünkü satırı (`adminreset.go:574-575`); değeri yalnız bir Go
  tür adıdır (`%T`; e-posta kanalında `*mail.SendError`).
- `err` — davet yolunun bugünkü satırı (`employeeactions.go:419`); değeri bu deponun kimlik
  taşıyan `fmt.Errorf` sarmaları ile kanalın döndürdüğü hatadır, ve e-posta kanalı
  `*SendError` (metni sınıf + kod, §3) ve kimlikler döndürür — başka metin sarmaz. **Tek
  istisna:** `ErrNotConfigured` (§2; `*SendError` değil, sabit ve değersiz bir metin — `nil` ya
  da `New` ile kurulmamış bir `SMTP`, yani bir programcı hatası).

Adres, link, sunucu metni, panik değeri, SMTP kullanıcı adı ve parolası bu kümede **yoktur**.

**m10'dan sapma, açıkça:** m10 §4'ün listesi `tenant_id, employee_id/admin_user_id,
message_id, class, smtp_code` idi. Bu ADR iki şey ekler: `invite_id`/`reset_id` (opak UUID'ler;
log satırını audit satırına bağlamanın tek yolu; `Ref`'in karakter kümesi onların adres ya da
URL taşıyamayacağını garanti eder — §2), `ip`, `err_type` ve `err` (bugünkü iki satırda zaten
var; değerleri yukarıda sınırlandı).

### 11. SES yapılandırması (kullanıcının dış adımları 5, 8 ve yeni adım)

- Yapılandırma seti `tappa-transactional`: TLS **Require** (K10), **açılma ve tıklama izleme
  KAPALI** — tıklama izleme linki AWS'nin yönlendiricisinden geçirir, yani sırrı (davet kodu,
  sıfırlama token'ı) üçüncü bir URL'ye ve onun kayıtlarına taşır; açılma izleme bir piksel
  ekler (§8 piksel yasağı). İki davranış SES belgesine dayanır, ölçülmedi; izlemenin kapalı
  olduğu EM-5'in canlı dumanında gelen iletinin kaynağından okunur (link `taptime.mt`'ye
  gider, piksel yok). Hesap düzeyinde bastırma listesi (BOUNCE + COMPLAINT) açık. Set
  kimliğe **varsayılan** olarak atanır; kod `X-SES-CONFIGURATION-SET` başlığı göndermez.
- **Geri bildirim yönlendirmesi (feedback forwarding) — yeni dış adım:** hedef **kapalı**.
  Bounce ve complaint bildirimleri özgün iletinin başlıklarını ve gövdesini, yani **linki**
  taşıyabilir (SES belgesi; **ölçülmedi**). SES yönlendirmenin kapatılması için bounce ve
  complaint'e SNS konusu bağlanmasını isteyebilir (belge; ölçülmedi) ve SNS EM-10'dur; o hâlde
  EM-10'a dek yönlendirme açık kalırsa **bildirim adresi** yalnız operatörün okuduğu bir kutu
  olur (paylaşılan ya da üçüncü taraf bir kutu değil) ve link taşıyabilen bir yer olarak
  sayılır (sayılı sınır 14). Bildirim adresi dış adım olarak yazılır.
- IAM: yalnız `ses:SendRawEmail`, kaynak: identity + yapılandırma seti ARN'si, koşul
  `ses:FromAddress = no-reply@taptime.mt`.
- SMTP kimlik değerleri yalnız Secret'a yazılır; hiçbir belgeye, commit'e, sohbete yazılmaz
  (m10 dış adım 9, Olay A-0).

### 12. Bugünkü durum, geçiş ve geliştirme ortamı (EM-K9)

- **EM-K9 kararı:** yerel posta yakalayıcı (Mailpit) **yok**, docker-compose'a servis
  eklenmez. Geliştirmede e-posta **gönderilmez**: dev ve test ortamlarında `none`/`panel`
  modları kalır. Taşıyıcı **Go testlerindeki öz-imzalı sahte SMTP sunucusuyla** doğrulanır
  (EM-2), sonra **doğrudan SES'e** bağlanılır — önce sandbox, doğrulanmış alıcılarla (EM-5).
  Böylece *"geliştirmede STARTTLS sertifikasına nasıl güvenilir"* sorusu düşer; prod'da özel
  kök havuzunun yasağı §2'dedir.
- **Kabul edilen risk — dev'de `email` modu (güvenlik DÜŞÜK, 3. tur; orkestratör kararı).**
  *"Geliştirmede e-posta gönderilmez"* mekanik bir kapı **değil**, bir varsayılandır: dev'de
  `TAPPA_*_DELIVERY=email` boot hatası **vermez**, çünkü kullanıcı *"ses'e baglariz direkt"*
  dedi ve dev'den SES sandbox'ına bilinçli bir deneme engellenmemeli. Bedeli, sayılı:
  - `email` modu dev'de açılırsa ulaşılabilir tek hedef **gerçek bir röledir** (yerel
    yakalayıcı yok, §2'nin kök havuzu yasağı test dışı özel köke izin vermez).
  - **Prod IAM kimliği `.env`'e konmaz.** `.env` depoda yok sayılır ve R7d'nin taradığı dosya
    listesine girmez (B36) — oraya yazılan bir değeri hiçbir tarama görmez.
  - Seed'deki **37** adres (`@kebabfactory.mt`, `@kebabmfg.mt` — B36) seed'li bir dev
    veritabanında gerçek davet alabilir (bu alan adlarının posta kabul edip etmediği
    ölçülmedi).
  - **Öneri (yeni dış adım):** yerelden SES denemesi için ayrı, sandbox'ta kalan, kısıtlı bir
    IAM kimliği (sandbox yalnız doğrulanmış alıcılara gönderir — SES belgesi, ölçülmedi —, bu
    da seed adreslerini korur). Sayılı sınır 25.
- EM-2…EM-4 davranış değiştirmez (`none`/`panel`). Davranış iki ConfigMap değişikliğiyle
  açılır: sıfırlama EM-5 sevk edilip kullanıcının dış adımları (1–10 ve geri bildirim adımı)
  bitince, davet EM-7 sevk edilince. Her ikisi bir **deploy kararıdır** (`main`'e birleştirme —
  CLAUDE.md §10).

### 13. Kapsam dışı (sayılı)

1. Bounce/complaint işleme (SNS → HTTPS, imza doğrulama) — EM-10, kendi ADR'si (K8).
2. Signup e-posta doğrulaması (ADR 0013 (c)) — EM-11, kendi ADR'si (K11). Q02'nin cevabı adres
   **doğrulamasını** getirmez: 00017'nin yazılı artık-kilidi (M7-04 kart düzeltmesi 3:
   *"kapatan şey aynı: adres doğrulama"*) bu ADR'yle kapanmaz.
3. Yönetici daveti (M7-07) — EM-12, kendi kartı ve ADR'si.
4. Rapor e-postası, Q28 uyarı teslimi, SMS, BIMI/MTA-STS.
5. *"X via Taptime"* gönderen adı ve e-postada logo — ADR 0023 §8 ve *Karar verilmedi*.
6. Tenant'a özel Reply-To.
7. Yerel posta yakalayıcı (EM-K9).

## Güvenlik iddiaları — üç parçalı

PART I'deki *"ölçülecek"* davranışların testleri (EM-3…EM-7) henüz yoktur; adları kendi
görevlerinde konur, burada **tarifleriyle** yazılır. EM-2'nin testleri `internal/mail`'dedir
(4. turdan beri bu worktree'de takip dışı kopya; birleştirmede EM-2'nin ağacı geçerli) ve
**adlarıyla** anılır; *"ölçüldü"* diyen EM-2 cümleleri EM-2'nin kendi koşularıdır, bu görevde
yeniden koşturulmadı; mutasyon sonuçları (M-numaraları) EM-2'nin ya da denetçinin
ölçümleridir. S1–S14 go1.27.1 ve go1.26.6'da alındı.

**İddia A — SMTP kimlik bilgisi TLS kurulmadan gönderilmez; STARTTLS yoksa gönderim olmaz.**
- **PART I:** S2, S3 (bugün ölçüldü: stdlib'in kendi koruması `localhost` adında kimliği düz
  metinde gönderiyor), S12. EM-2'de ölçüldü: STARTTLS ilan etmeyen, STARTTLS'i 454 ya da 502
  ile reddeden, güvenilmeyen sertifika sunan, **güvenilen ama başka bir ada verilmiş**
  sertifika sunan, **aranan adı taşımayan** sertifika sunan ya da en çok TLS 1.1 konuşan test
  içi sahte sunucu → sınıf `tls_unavailable`, sunucu **0** `AUTH` ve 0 `MAIL` görür — sunucu
  adı `127.0.0.1` ve `localhost` iken; kontrol satırları: TLS 1.2'de ve **tam aranan adı
  taşıyan** sertifikayla 1 AUTH.
- **PART II:** `TestSend_NoTLSMeansNoAuth` (iki sunucu adı × satırlar; sertifika adı satırları
  dahil) ve onun kontrolü olan stdlib ölçümü `TestPlainAuth_SendsInClearForTheLoopbackNames`.
  Yakaladıkları: STARTTLS şartının kaldırılması; şartın stdlib'in `PlainAuth` kontrolüne
  bırakılması (`localhost` satırları); `MinVersion`'ın açıkça 1.2'nin altına indirilmesi (TLS 1.1
  satırı); sertifika doğrulamasının kapatılması (güvenilmeyen sertifika satırı); `ServerName`'in
  sabitlenmesi (denetçinin M09'u kırmızı) ve **adın doğrulanmaması** — zincir doğru, ad yanlış
  (başka ada verilmiş ve adı taşımayan sertifika satırları). **Yakalamadığı:** `MinVersion`'ın hiç **konmaması** — S12: bugünkü stdlib
  varsayılanı aynı sonucu verir, davranış farkı yoktur; açık yazım kod incelemesinin konusudur
  (istenirse `internal/mail`'deki her `tls.Config` bileşik değerinin `MinVersion` taşıdığını
  okuyan bir kaynak pini tarif edilebilir — bu turda istenmedi).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia B — gönderim hatası ve `Receipt.MessageID` sunucu metni, adres ya da link taşımaz.**
- **PART I:** S6 ve B11 (bugün ölçüldü: sarılan sunucu hatası adresi ve `?t=` değerini taşıyor,
  davet yolu zinciri logluyor). EM-2'de ölçüldü: alıcıyı adlandıran ve
  link taşıyan redlerde `Error()`, `%v`, `%+v`, `%#v`, `%s`, `%q`, bir `%w` sarması, slog
  metin/JSON ve `encoding/json` ne adresi ne linki taşır; `*textproto.Error`'a ulaşılamaz.
  EM-7'de ölçülecek: aynı yanıt `employeeactions.go:419` yolundan geçince log satırında ikisi
  de yok. **`message_id` (3. tur, §2'nin pencere kuralı):** bugün B33 (denetçinin ölçümü — 2.
  turun birebir alt dizge kuralı `x<token>` ve `<token>-000000`'ı geçirdi). EM-2'de ölçüldü:
  ön ya da son ekli link değeri (`x<kod>`,
  `<kod>.1`, `<kod>-000000` biçimleri), ön ekli parola (`x<parola>`), birebir ve parça olarak
  AUTH argümanı, ön ek olarak kullanıcı adı, son ekli alıcı yerel kısmı, yalnız telde bulunan bir
  parça, QP yumuşak satır sonuyla bölünmüş bir parça, RFC 2047'nin değiştirdiği bir konu parçası
  ve `Ref` taşıyan id'ler `""` döner; gerçekçi bir SES id'si korunur (kontrol).
- **PART II:** biçim sızıntı testi `TestSendError_CarriesNoServerText` · `message_id` tablo
  testi `TestSend_ReadsTheMessageIDFromThe250Reply` · reflect tabanlı alan + yöntem pini
  `TestSendError_HasOnlyAClassAndACode` · EM-7'nin panel log sızıntı testi ·
  `TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText` (sıfırlama yolu, bugün sevk
  edilen). Yakaladıkları: biçim testi — `Error()`'un sunucu metnini biçimlemesi, `errors.As`
  ile `textproto.Error`'a ulaşılması, listelenen biçimlerden birinde adres ya da link; reflect
  pini — **herhangi** bir yeni alan (`Error()`'ün basmadığı `error` ya da `[]byte` alanı dahil,
  ki biçim testi onu göremez — §3), `Unwrap`/`Is`/`As`/`Format` yöntemi; EM-7 — davet yolunun
  kanal metnini loglaması; sıfırlama pini — `err_type` yerine hatanın loglanması; `message_id`
  testi — yankı kuralının ya da yalnız pencerenin kaldırılması (EM-2'nin M21, M36 mutasyonları
  kırmızı), ham ileti, kullanıcı adı, `Subject`, `Ref`, `Text`, `HTML`, parola ya da AUTH
  argümanının listeden düşmesi (M37, M38, M41–M46 kırmızı). **Yakalamadığı:** `To` ya da
  `From`'un listeden düşmesi — ölçülmüş eşdeğerlik, ham ileti onları kapsar (M39, M40 yeşil;
  §2); sayılı sınır 17'nin kaçışları.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C — başlık ve alıcı enjeksiyonu bağlantı açılmadan reddedilir.**
- **PART I:** S7, S8, S9 (bugün ölçüldü). EM-2'de ölçüldü: §4'ün red
  listesindeki her biçim (CR, LF, NUL, DEL, TAB, C1, U+2028/U+2029, geçersiz UTF-8,
  encoded-word şekli, görünen ad, köşeli parantez, yorum, boşluk, liste, tırnaklı yerel kısım,
  ASCII dışı adres, 255 baytlık adres, geçersiz `Ref`, boş ya da geçersiz gövde, uzun başlık
  satırı; 3. turdan: `To` içinde `=?`) kendi sınıfıyla reddedilir ve sunucu **0** bağlantı kabul
  eder; görünen adlı ya da geçersiz bir `Reply-To` `New`'de reddedilir; artı-adresleme, büyük
  harf, köşeli parantezli alan adı ve 254 baytlık adres kabul edilir ve RCPT'ye birebir ulaşır;
  üretilen girdilerde bağımsız bir kâhinle karşılaştırılan fuzz testi.
- **PART II:** dial öncesi red tablo testi `TestSend_RefusesBadInputBeforeAnyDial` · fuzz testi
  `FuzzCompose` · son başlık kapısı testi `TestHeaderBlock_RefusesWhatItCannotWriteVerbatim`.
  Yakaladıkları: reddetmek yerine temizlemek ya da kodlamak;
  `Address == girdi` karşılaştırmasının düşmesi; ASCII bayt kuralının düşmesi; birden çok
  alıcı; `Subject`'te ya da `To`'da `=?` kuralının düşmesi; `Reply-To`'nun alıcı kuralı yerine
  gönderen kuralıyla doğrulanması; dial'dan sonra doğrulama.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia D — SMTP kimlik bilgileri (parola ve kullanıcı adı, `mail.Credential`) ve `Message`
kendi yazdırma yollarında redakte edilir.**
- **PART I:** EM-2'de ölçüldü: `Credential` için testin listelediği `fmt` fiilleri, `String`,
  `GoString`, slog metin/JSON ve `encoding/json` — doğrudan, `Config` içinde, kurulmuş `*SMTP`
  içinde ve çağıranın dışa kapalı alanında; değer de ilk 8 karakteri de görünmez; kasıtlı
  yansımanın değeri okuduğu da ölçüldü (sınır). `Message` için **testin listelediği biçimler**
  yer tutucuyu basar ve ne alıcıyı ne link değerini (ne de ilk 8 karakterini) taşır:
  `String()`, `GoString()`, `%v`, `%+v`, `%#v`, `%s`, `%q`, işaretçi (`%+v`), dilim (`%v`),
  çağıranın **dışa açık** alanında `%+v`, slog metin ve JSON işleyicileri, `encoding/json`;
  `slog.AnyValue(m).Resolve()`, `%x` ve `%d` de (EM-2'nin kapanış ölçümünde bu biçimleri bozan
  mutasyon — onun numaralamasıyla M14 — kırmızı). Dışa kapalı bir alandaki
  `Message`'ın alanlarıyla basıldığı ölçüldü (sınır). Listede olmayan bir biçim bu iddianın
  dışındadır. EM-3'te: kimlikleri taşıyan `config.Config` değeri aynı biçimlerde.
- **PART II:** derleme zamanı doğrulamaları — beş redaksiyon arayüzünün her biri için bir
  `var _ <arayüz> = Credential{}` satırı (EM-2'de `credential.go`) ve aynı beşi `Message{}`
  için (EM-2'de `mail.go`); emsal `internal/invite/code.go:122-128`, `Code` için aynı beş satır
  · `TestCredential_IsRedactedInEveryRendering` · yansıma sınırı testi
  `TestCredential_KnownLimitIsReflection` · `Message` yer tutucu testi
  `TestMessage_PrintsAsAPlaceholder` · `Message`'ın dışa kapalı alan sınırı testi
  `TestMessage_KnownLimitIsAnUnexportedField` · EM-3'ün config redaksiyon vakası.
  Yakaladıkları: bir redaksiyon yönteminin silinmesi (derleme hatası; EM-2'nin M15a'sı
  derlenmedi); bir biçimde değerin görünmesi (M14, M15b; `Message`'ta M54, M55 kırmızı);
  dolaylı `*string` alanının düz `string`'e çevrilmesi (dışa kapalı alan satırı); kimliğin
  `Config`'te düz `string` olarak tutulması (EM-3). **Yakalamadığı:** kasıtlı yansıma, hata
  ayıklayıcı, çekirdek dökümü (sayılı sınır 26); dışa kapalı alandaki `Message` ve alanlarını
  doğrudan loglayan çağıran (sınır testleri bunu **sınır olarak** ölçer, kapatmaz — sayılı
  sınır 20).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia E — sıfırlama yanıtının süresi gönderime bağlı değildir.**
- **PART I:** bugün `TestAdminReset_TimingIsFlat` 15 ms kanal gecikmesiyle yeşil (B7). EM-5'te
  ölçülecek: kanal gecikmesi 2 s iken kayıtlı ve kayıtsız adres medyanları
  [taban, taban + 50 ms] içinde; aynı test bugünkü senkron kodda **kırmızı** (kartta
  gösterilir).
- **PART II:** EM-5'in yeni zamanlama testi · `TestAdminReset_TimingIsFlat`. Yakaladıkları:
  yeni test — gönderimin istek yoluna geri taşınması, isteğin işçiyi beklemesi; mevcut test —
  tabanın kaldırılması (iki kolun da tabanı ödediğini ve medyan farkının ≤ 60 ms olduğunu
  ister; 15 ms'lik kanalı tabanın altında kaldığı için senkron gönderimi **yakalamaz**).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia F — süreç ölmediği ve hesap bütçesi aşılmadığı sürece her sıfırlama grant'ı tam bir
audit satırıyla biter.**
- **PART I:** bugün senkron yol bunu yapıyor (B5) ve bütçe kuralı ölçülmüş (B8). EM-5'te
  ölçülecek: başarı, gönderim hatası, kuyruk dolu, işçide panik ve kapanış boşaltması
  yollarında grant başına tam 1 satır; §6.6(b)'nin davranış testiyle — T ≥ D süren bir istek
  uçuştayken — dizinin toplam süresinin T + D − pay'ın altında kaldığı (denetçinin T = D = 1 s
  ölçümü: doğru 1,056 s, mutasyonlar 2,089 s ve 2,141 s — B34).
- **PART II:** EM-5'in grant başına audit testi (beş yol) · EM-5'in boşaltma süresi yuvalanma
  testi (sabit) · EM-5'in kapanış dizisi davranış testi (§6.6(b)). Yakaladıkları: bu beş yolda
  0 ya da 2 satır; boşaltma sabitinin `httpShutdownGrace`'i aşması; kapanış dizisi
  **işlevinin içinde**, pay'den uzun ardışık bir bekleme (boşaltmanın Shutdown döndükten sonra
  başlaması dahil). **Yakalamadığı:** süreç ölümü (sayılı sınır 1); bütçe aşımı (B8'in bugünkü
  kuralı); DATA sonlandırıcısından ya da 250'den sonraki iptal veya panik (sayılı sınır 15 —
  satır yazılır ama yanlış olabilir); kapanış dizisi işlevinin **dışında** `main`'e eklenen bir
  bekleme — mevcut kapanış testleri yalnız sabit okur (B18: `time.Sleep(3s)` ile üçü de PASS);
  pay'den kısa (≤ ~500 ms) bir ek bekleme; yalnız testin kurmadığı bir koşulda ödenen bir
  bekleme (§6.6).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia G — `email` modunda yönetici linki görmez; adresi olmayan, geçersiz ya da yönetici
adresine eşit çalışana kod basılmaz.**
- **PART I:** bugün tek kanal `ManagerVisibleChannel` (B10). EM-7'de ölçülecek: yanıt gövdesinde
  `/activate?code=` 0; `invite.code_emailed` 1, `invite.code_shown_to_manager` 0; üç ret
  durumunda (büyük/küçük harf farklı yönetici adresi dahil) `employee_invites` satırı 0;
  sınırda N+1. davet reddi, satır basılmaz; yedek owner olmayana kapalı (bir `manager` satırı
  test fikstürüyle kurularak — B28: üründe yok). EM-6'da: A tenant'ının bağlantısı B'nin
  adresini okuyamaz; değişiklik bekleyen davetleri aynı transaction'da iptal eder.
- **PART II:** EM-7'nin gövde testi · audit sayım testi · ret tablo testi · oran testi · rol
  testi · EM-6'nın RLS izolasyon testi ve aynı-tx iptal testi. Yakaladıkları: `email`
  modunda kodun ekrana çıkması; basımın adres kontrolünden önce yapılması; yönetici adresi
  karşılaştırmasının atlanması ya da büyük/küçük harfe duyarlı yapılması; oran sayacının
  atlanması; yedeğin owner olmayana açılması. **Yakalamadığı:** artı-adresleme, takma ad,
  ikinci posta kutusu (ADR 0005 §5 ek notu; sayılı sınır 2); bugün üründe owner olmayan
  yönetici olmadığı için rol kapısının üründe hiçbir isteği ayırmaması (B28).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia H — şablonun e-posta gövdesine yazdığı literal mutlak URL tektir ve gövde uzak kaynak
yüklemez.**
- **PART I:** EM-4'te ölçüldü (2026-10-03; ayrıntı §8'in EM-4 notu ve `web/templates/email`'in
  paket belgesi): davet ve sıfırlamada her parçada tam 1 mutlak URL ve o da link (HTML'de tek
  `href`, düz metinde kendi satırında bir kez), düz ve düşmanca ad kümesiyle
  (`TestRender_EachPartCarriesExactlyOneAbsoluteURL`); link `BaseURL` + yol + `?<param>=` +
  base64url değilse red (`TestRender_RefusesALinkOutsideTheExpectedAddress`); `<img`,
  `<link`, `<script`, `<style`, `@font-face`, `@import`, `url(` ve `src`/`srcset`/`background`
  özniteliği 0, öğeler tam olarak html, head, meta, title, body, div, p, h1, a
  (`TestRender_LoadsNothingAndUsesOnlyTheseElements`); `<script>`, tırnak ve `&` kapı atlanarak
  şablona verildiğinde kaçışlı (`TestTemplate_EscapesWhateverReachesIt`); Maltaca ad (ċ ġ ħ ż,
  iki harf büyüklüğü) iki parçada UTF-8 olarak doğru (`TestNames_AnOrdinaryNameIsShownVerbatim`);
  sabit ASCII konu `mime.QEncoding`'den değişmeden geçer, Maltaca girdi `=?utf-8?q?` olur (S9)
  ve ileti `internal/mail`'in derleyicisinden geçer
  (`TestSubject_IsFixedASCIIAndPassesTheMailComposer`); testin **listelediği** düşmanca ad kümesi
  gizlenir ve o kümede gövdenin görünen metninde linkten başka linklenebilir dizi 0
  (`TestNames_AnAddressShapedNameIsWithheld`).
- **PART II:** yukarıdaki testler; her birinin yakaladığı mutasyonlar M10 EM-4 kartında
  (izleme pikseli, uzak yazı tipi, ikinci link, `templ.Raw`, linkin düz metinden düşmesi, kapının
  kaldırılması ya da gevşetilmesi, konuda ad, palet dışı renk, AA altı çift). **Yakalamadığı:**
  istemcilerin gerçekte neyi linklediği (ölçülmedi; dedektör bizimdir); dedektörün etiket
  ayracı olmayan, noktaya benzeyen her karakter (harf, işaret, rakam), rakamlar ve düz
  saldırgan düzyazısı gösterilir (`TestNames_KnownLimitIsALookalikeDotAndPlainProse` — ölçülmüş
  sınır; sayılı sınır 22; EM-7'nin önünde ürün kararı).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia I — yapılandırma eksikse süreç açılmaz.**
- **PART I:** bugün `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed` (B2). EM-3'te ölçülecek:
  her eksik anahtar ayrı bir vaka olarak boot hatası; `465` **her ortamda** (dev dahil) red;
  `localhost`, `*.localhost` ve IP literal **prod'da** red; iki akış kapalıyken eksik SMTP
  anahtarlarıyla boot başarılı; test dışı kodda `mail.Config`'in `RootCAs` alanı atanmamış
  (atama ve anahtarlı bileşik değer); hizmet konteynerinin manifestinde `SSL_CERT_FILE` /
  `SSL_CERT_DIR` env'i 0 (§2'nin pinleri a–c).
- **PART II:** EM-3'ün fail-closed matris testi · `mail.Config` kaynak pini · manifest
  `SSL_CERT_*` pini · `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest`.
  Yakaladıkları: eksik anahtarla açılan süreç; ortamlardan birinde `465`'in kabulü; manifestte
  olmayan yeni değişken; test dışı Go kodunda `mail.Config`'e özel kök havuzu (iki yazım
  biçimiyle); manifestte kök konumunu değiştiren env. **Yakalamadığı:** `/etc/ssl/certs` ya da
  imajın kök deposu üzerine bağlanan bir volume veya değiştirilmiş bir imaj — Go ataması da env
  de olmadan kök havuzunu değiştirir (B32; sayılı sınır 23); `TAPPA_SMTP_HOST`'u değiştirebilen
  birinin röleyi, herkese açık güvenilir sertifikası olan kendi sunucusuna çevirmesi (izinli ad
  listesi yok — sayılı sınır 24); kurulum kodunun `mail.Config` dışında bir yolla `tls.Config`
  kurması (pin bilerek `mail.Config`'e daraltıldı).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia J — erişilebilirlik: bir gönderim denemesi sunucudan en çok 256 KiB okur (3. tur,
EM-2 sapması j).**
- **PART I:** bugün B31 (kaynak: `textproto` satırı sınırsız okur) ve S14 (ölçüldü: 32 MiB'lık
  bir TLS öncesi selamlama `smtp.NewClient`'te hatasız kabul edildi, ≈256–288 MiB ayırttı);
  güvenlik denetçisinin ölçümü (bu turda yeniden koşturulmadı): ~120 MiB'lık bir selamlama
  512Mi'lik tek replikayı OOM'a soktu. EM-2'de ölçüldü, tavanın 16 katı sel gönderen bir
  rölede beş vaka: (1) satır sonu olmayan tek satırlık
  selamlama; (2) 1 KiB'lık devam satırlarıyla selamlama; (3) boş devam satırlarıyla selamlama;
  (4) TLS öncesi EHLO yanıtı, 1 KiB'lık devam satırları; (5) **TLS üzerinden** veri sonu yanıtı,
  1 KiB'lık devam satırları. Her birinde sınıf `network`, yanıt kodu yok, gönderim süreden çok
  önce (≤ 2 s) döner ve el sıkışma dahil toplam ayırma tavanın 16 katı (4 MiB) bütçenin altında
  kalır — ölçülen 0,9–2,1 MiB. Ayrıca bir ağ hatası **yeniden denenmez** (veri sonu noktasından
  sonra düşen ve DATA ortasında sıfırlanan bağlantı, ikisi de tek bağlantı) — bayt tavanının
  `network` sonucu bu yüzden tek denemeye mal olur. Sayacın bütün denemeyi kapsadığı ayrıca
  ölçüldü (`TestSend_TheReplyCapCoversTheWholeAttempt`): (6) **bölünmüş** vaka — TLS öncesi
  200 KiB EHLO + TLS sonrası 100 KiB veri sonu yanıtı → `network`, oysa iki kontrol satırında
  her yarı tek başına başarılı (tek sayaç, STARTTLS'te sıfırlanmaz); (7) **el sıkışma** vakası —
  el sıkışma başladığında 64 bayt bütçe kalmışsa → `network` ve sunucu el sıkışmayı
  tamamlamaz, 64 KiB bütçeyle kontrol satırı başarılı; ikisinde de tek bağlantı. EM-2'nin
  ölçümü: `crypto/tls` sayacın hatasını `errors.Is`'in görebildiği biçimde geri verir, yani
  `classify` el sıkışma içindeki tavan aşımını tanır.
- **PART II:** bayt tavanı tablo testi `TestSend_CapsWhatTheRelayCanMakeItRead` · bütün deneme
  testi `TestSend_TheReplyCapCoversTheWholeAttempt` · ağ hatasında tek bağlantı testi
  `TestSend_ANetworkFailureIsNotRetried` · kesik 250 testi
  `TestSend_ACutReplyToTheEndOfDataIsAcceptance`. Yakaladıkları: sayacın kaldırılması
  (EM-2'nin M34'ü kırmızı) ya da `NewClient`'ten **sonra** takılması (selamlama vakaları); çok
  satırlı yanıtın ya da TLS sonrası 250 yolunun sayılmaması (EHLO ve veri sonu vakaları);
  tavanın büyük ölçüde büyütülmesi (M35, ×64, kırmızı — 4 MiB'lık ayırma bütçesiyle); ağ
  hatasının yeniden denenmesi (M51 kırmızı); sayacın STARTTLS'te **sıfırlanması** (M01 kırmızı —
  bölünmüş vaka); el sıkışmada tavan aşımının `network` dışında bir sınıfa düşmesi (M16 kırmızı
  — el sıkışma vakası); kesik bir 250'nin `network` sayılması (M66 kırmızı). **Yakalamadığı:**
  tavanın küçük büyütülmesi (bütçe testinin çözünürlüğü 4 MiB); tavan **deneme başınadır**, süreç başına değil — bir `Send` en çok 2 × 256 KiB okur
  (§2), eşzamanlı gönderimlerin toplamı (§6'nın 1–2 işçisi, davetin senkron gönderimleri) oran
  sınırlarıyla dolaylı sınırlanır; tavanın altında kalıp yavaş akan bir sunucu (süre sınırlar,
  §2).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## Elenen seçenekler

- **(b) SES v2 HTTP API + elle SigV4.** Yeni modül 0, ama imza protokolünü (kanonik istek,
  tarih/bölge/servis zinciriyle dört adımlı HMAC-SHA256 türetmesi) biz yazar ve bakarız; imza
  zaman damgası taşır, yani **sunucu saatimiz** bir teslimat bağımlılığı olur (AWS'nin kabul
  ettiği kayma ±5 dk — m10 tablosundan, ölçülmedi); test yolu yalnız SES'tir — aynı kodu test
  içi sahte bir sunucuda koşturacak bir eş yok. **§3 gerilimi eleme gerekçesi değildir:** m10
  tablosu *"HMAC zinciri bizde — §3 'kripto yalnız internal/sun' gerilimi"* diyordu; ölçüldü,
  `crypto/hmac` bugün `internal/sun` dışında 12 üretim dosyasında (B24) ve ADR 0020'nin
  *"Kriptografinin yeri"* kararı §3'ün kuralını fiilen AES / blok şifreye bağladı. Eleyen, imza
  protokolünün sahipliği ve yalnız-SES test yoludur.
- **(c) `aws-sdk-go-v2`.** Ölçüldü (S11): yalnız `sesv2` ile **6**, kimlik zinciriyle
  (`config`) **15** modül; bugün `go.mod` **10** modül gerektiriyor (B25). CLAUDE.md §1 yeni
  bağımlılık için sormayı ister; govulncheck yüzeyi ve sürüm temposu m10 tablosundan, yeniden
  ölçülmedi. Test yolu yine yalnız SES.
- **`smtp.SendMail`.** S2 (STARTTLS yoksa düz metin), S4 (Message-ID atılır), S5 (süre yok).
- **Senkron sıfırlama gönderimi.** B7: taban yalnız altında kalanı eşitler; ölçüm EM-5'te.
- **Linki kalıcı bir kuyrukta (DB outbox) tutmak.** B13'ün ikinci yükümlülüğü: link kalıcı
  saklanmaz; outbox ya linki ya da ham token'ı saklamak zorunda kalırdı.
- **Sunucu metnini temizleyip loglamak (denylist).** B6: altı sıradan biçimin ikisinde sızdı.
- **Örtük TLS (465).** Tasarım STARTTLS; bir portta iki el sıkışma biçimi desteklenmez.
- **Açılma/tıklama izleme.** §11.
- **Yerel posta yakalayıcı (Mailpit).** EM-K9, kullanıcı kararı (§12).
- **Postmark / Resend / kendi SMTP sunucusu** (Q02'nin listesi). Bu ADR'de **ölçerek
  karşılaştırılmadı**; m10 tablosu yalnız SES'in üç arayüzünü karşılaştırır. Seçim K7
  (`eu-central-1` SES) *"önerisiyle uygulanır"*.

## Sayılı sınırlar ve kabul edilen riskler

1. **Basım ile audit arasında süreç ölümü → audit'siz sıfırlama.** `password_resets` satırı en
   çok `ResetTTL` (1 saat) canlıdır; ölüm 250'den sonraysa link teslim edilmiş olabilir;
   `requested`/`undelivered` satırı ve `RetiredCount` yazılmaz. Link harcanırsa
   `admin.recovery.completed` yine yazılır. Bugünkü senkron yolda aynı pencere vardır, daha
   dar. Tampondaki grant sayısı pencereyi büyütür (§6.2).
2. **Y-D daralır, kapanmaz — ve iz dar.** Adresi müdür yazar; yönetici adresine `citext`
   eşitliği yalnız birebir durumu kapatır. **Tek bir kutu N hayali çalışana yeter:**
   artı-adresleme (`boss+g1@…`) eşitlik kapısını da tenant içi tekil indeksi de geçer. Kayıtta
   alıcı adresi **yalnız `employees` satırındadır ve adres düzenlenene dek**; EM-6'nın satırı
   değişikliğin olduğunu gösterir, değeri göstermez; `invite.code_emailed` ve `employee.added`
   adres taşımaz (B29). Müdür adresi kendi kutusuna çevirip aktive ettikten sonra geri
   çevirirse iz yalnız *"değişti"* der. ADR 0005 §5 ek notu.
3. **SES sandbox.** Üretim erişimi gelene dek yalnız doğrulanmış alıcılara gönderilebilir; red
   metninin alıcı adresini taşıması §3'ün gerekçelerinden biridir. SES davranışı **ölçülmedi**.
4. **"Gönderildi" = SES 250, gelen kutusu değil.** Bounce/complaint işlenmez (EM-10'a dek);
   bastırılmış adrese gönderimin sessiz kalması m10 §7'de risk olarak yazılı, **ölçülmedi**.
   Ekran cümlesi *"sent to"* der, *"delivered"* demez.
5. **SES'in bizim `Message-ID` başlığımıza ne yaptığı ve 250'de kendi id'sini döndürmesi**
   ölçülmedi (EM-5).
6. **Devre kesici paylaşımlıdır ve SES hesabı tektir.** Sıfırlama yayılımı (kaynak adres başına
   10 dakikada 160 — B9) **ve** davet yolu (ömür boyu tavansız kayıt; tek adres ikinci saatte
   kesiciye ulaşır — §9) kesiciyi tüketebilir ve pencere boyunca bütün gönderimleri durdurur.
   Aynı akış SES hesabının itibarını (bounce/complaint oranları) düşürebilir; SES eşik aşımında
   hesabın gönderimini **duraklatabilir** (SES belgesi; ölçülmedi) — o zaman bütün tenant'ların
   sıfırlama ve davet e-postası durur. m10 dış adım 11'in alarmları (`BounceRate > 0.02`,
   `ComplaintRate > 0.0005`) erken uyarıdır, önlem değildir. Sıfırlama için alıcı başına
   gönderim tavanı **karar verilmedi**: M7-04 düzeltme 1(iii)'ün ölçtüğü kurtarma reddini
   yeniden üretir.
7. **Alan adı denetimi yok.** Köşeli parantezli alan adı ve herhangi bir alan adı alıcı
   kuralından geçer (S8); MX denetimi yapılmaz. Müdürün denetlediği bir alan adı Y-D artığıdır.
8. **iOS uygulama-içi tarayıcı çerez ayrımı** ölçülmedi — EM-8 gerçek cihaz turu.
9. **Hetzner'in 587 çıkışı** ölçülmedi; 25/465 engeli m10'dan. İlk gönderim (EM-5) doğrular.
10. **SMTP kimliği uzun ömürlü bir IAM anahtarıdır;** döndürme elle (dış adım 8). R7d'nin
    yakalamadığı biçimler m10 §4'ün 2026-09-25 kart düzeltmesinde sayılı.
11. **Davet teslimatı commit'ten sonra** (B12) — e-postayla daha sık görülebilir.
12. **Saklama kuralı gönderim kuralından zayıf** (B14, §4): bazı saklanmış adresler e-posta
    alamaz; davette net cümleyle reddedilir, migration yok.
13. **Q02'yi açık diye anan kod metni bayatlar** (B26): orkestratör Q02'yi "Cevaplananlar"a
    taşıdığı anda o satırların *"Q02 açık"* yarısı yanlış olur; *"taşıyıcı yok"* yarısı EM-5'e
    dek doğrudur. Dokundukları dosyalarda EM-3/EM-5/EM-7 düzeltir; uygulanmış migration
    yorumları (00017, 00019) değiştirilmez. Sayım komutla tekrarlanır (B26).
14. **Geri bildirim bildirimleri linki taşıyabilir** (§11; ölçülmedi). EM-10'a dek yönlendirme
    kapatılamazsa bildirim adresi link taşıyabilen bir yerdir.
15. **DATA sonlandırıcısından sonra iptal ya da süre dolması.** İstemci `.\r\n`'i yazdıktan
    sonra 250'yi okuyamadan iptal ya da süre dolarsa (`timeout`), SES iletiyi **kabul etmiş
    olabilir**: link gitmiştir ama sıfırlamada `undelivered`, davette başarısızlık satırı ve
    ekranda *"gönderilemedi"* yazılır. Yön: kayıt eksik değil **yanlış** — gönderilmemiş
    görünen bir link canlıdır (sıfırlamada en çok 1 saat, davette 7 gün). Davette yöneticinin
    yeniden basması eski kodu emekliye ayırır (B12), yani zarar orada kendini sınırlar. **Aynı
    yön, ikinci yol:** 250 ile audit yazımı arasında işçide bir panik (§6.4) linki göndermiş
    ama `undelivered` yazmış olur.
16. **`SetDeadline` tek başına pinli değildir** (EM-2 sapması i): `AfterFunc` aynı süreye
    ulaştığı için testler ikisini ayırt edemez; `SetDeadline`'ın kaldırılması bütün testleri
    yeşil bırakır (EM-2 ölçtü).
17. **`message_id` pencere kuralının kaçışları (3. turda yeniden yazıldı):** kural, id ile
    gönderilen bir değer arasında **8 karakterlik ortak `[A-Za-z0-9._-]` penceresi** arar
    (§2). Yakalanmayanlar: gönderilen bir değeri **biçim değiştirerek** (base64, büyük/küçük
    harf, başka bir kodlama) izinli şekle sokan bir sunucu; bir değeri **8 karakterden kısa
    parçalara** bölüp parçaları **ayraçla** (sırası korunarak, ör. her 7 karakterde bir `.`)
    **ya da sırası bozularak** dizen bir sunucu — her parça tek başına bir sırrın 7
    karakteridir; **gömülü ve 8'den kısa** bir parça; değer listesinin dışında kalan, telde
    giden değerler: **EHLO argümanı** (`localhost`) ve TLS'in **SNI**'ındaki röle adı — sır
    değildirler, ama liste onları kapsamaz ve kendiliğinden genişlemez. **Ölçüldü:**
    `TestMessageID_KnownLimitIsACutOrReorderedEcho` — her 7 karakterde bir `.` ile sırası
    korunarak bölünmüş ve ters çevrilmiş bir link değeri pencere kuralından **geçer**; kontrol:
    8 karakterlik kesintisiz bir dizi yakalanır. Sınır sürdükçe test yeşildir; kapanırsa
    kırmızıya döner ve bu metin güncellenir.
18. **Owner-only yedek kısıtı EM-12'ye dek boştur** (B28); daralma o güne dek kanal ayrımıdır.
19. **Audit'te alıcının değeri yok** (sayılı sınır 2'nin kaydı): bir soruşturma, kodun gittiği
    adresi `employees` satırı düzenlendikten sonra kayıttan çıkaramaz. Anahtarlı özet seçeneği
    *Karar verilmedi*'de.
20. **`Message` üçüncü taşıyıcıdır; redaksiyonunun istisnası ölçülmüş** (§2): kendi yazdırma
    yolları yer tutucu basar, ama çağıranın yapısının **dışa kapalı** alanında tutulan bir
    `Message` yansımayla alanları dahil basılır (EM-2 ölçtü) ve `m.Text`/`m.To`'yu doğrudan
    loglayan bir çağıran link ve adresi loglar. İkisi kod incelemesinin konusudur.
21. **Pencere kuralının yanlış pozitifi:** meşru bir message-id, gönderilen bir değerle (ör.
    gövdedeki rastgele token ya da bizim `Message-ID` başlığımız) tesadüfen 8 karakterlik bir
    pencere paylaşırsa düşer. Sonucu **yalnız** `message_id`'nin kaybıdır (log satırı ve audit
    detail'i boş id taşır); gönderim ve audit sonucu değişmez.
22. **DKIM imzalı içerik kötüye kullanımı** (§8; güvenlik ORTA): açık ve doğrulamasız kayıt +
    serbest metinli tenant adı + keyfi adreslere davet → Taptime alan adıyla SPF/DKIM/DMARC'tan
    geçen, gövdesinde saldırganın seçtiği metin ve istemcinin link yaptığı bir URL bulunan bir
    oltalama e-postası. Oran sınırları (§9) hacmi sınırlar, içeriği değil. Tasarım kararı
    EM-4/EM-7/WL-11'de; istemcilerin otomatik linklemesi ölçülmedi. **EM-4 (2026-10-03):**
    gövdedeki ad bir izin listesinden geçmezse gizlenir (§8'in EM-4 notu) — testin listelediği
    düşmanca küme gövdeye **girmez**; **kalan:** dedektörün etiket ayracı olmayan, noktaya
    benzeyen her karakter (harf, işaret, rakam), rakamlar (telefon numarası, tam genişlikli
    rakamlar) ve düz saldırgan düzyazısı gösterilir, istemci davranışı ölçülmedi
    (`TestNames_KnownLimitIsALookalikeDotAndPlainProse`). **EM-7 `email` modunu açmadan önce**
    kullanıcının ürün kararı gerekir: adı yalnız doğrulanmış tenant'ta göstermek ya da bu
    riskin açıkça kabulü.
23. **Kök havuzu yasağının ortam yolları** (B32): `RootCAs: nil` Linux'ta sistem köklerini
    okur; manifestteki `SSL_CERT_FILE`/`SSL_CERT_DIR` env'ini §2'nin (c) pini yakalar, ama
    `/etc/ssl/certs` üzerine bağlanan bir volume, değiştirilmiş bir imaj ya da imaj içindeki kök
    deposu Go ataması ve env olmadan aynı sonucu verir — pinsiz, kod ve manifest incelemesinin
    konusu.
24. **`TAPPA_SMTP_HOST` için izinli ad listesi yok:** ConfigMap'i değiştirebilen biri röleyi,
    herkese açık güvenilir sertifikası olan kendi sunucusuna çevirebilir; TLS doğrulaması geçer
    ve SMTP kimliği ile her link o sunucuya gider. Bunu yapabilen yetki bugün Secret'ı da
    okuyabiliyorsa yeni bir yetki değildir — ConfigMap ve Secret yetkilerinin kümede aynı
    olduğu **ölçülmedi**.
25. **Dev'de `email` modu boot'ta reddedilmez** (§12, orkestratör kararı): dev'de açılırsa hedef
    gerçek bir röledir; `.env`'e yazılan bir prod kimliğini R7d görmez (B36); seed'in 37 adresi
    gerçek davet alabilir. Önerilen önlem dış adımdır (sandbox'ta kısıtlı ayrı IAM kimliği), kod
    kapısı değildir.

EM-2'nin kartındaki 12 sayılı sınırın bu ADR'de karşılığı olmayanlar (eşitleme turu) —
karşılığı olanlar: köşeli alan adı → 7 · dönüştürülmüş/kısa yankı ve yanlış pozitif → 17, 21 ·
`SetDeadline` → 16 · dışa kapalı alandaki `Message` → 20:

26. **Kimlik bilgisi kasıtlı yansımayla okunur** (EM-2 ölçtü): redaksiyon kazara yazdırmaya
    karşıdır; kasıtlı yansıma, hata ayıklayıcı ve çekirdek dökümü değeri okur.
27. **Tavan altındaki ayırma** EM-2'nin ölçümünde 0,9–2,1 MiB (tavanın 16 katı sel, beş vaka);
    tavan deneme başınadır, eşzamanlı denemelerin toplamı bununla sınırlanmaz (İddia J).
28. **Açık `MinVersion` bugün Go varsayılanına eşittir** (S12): satırın silinmesi bütün testleri
    yeşil bırakır (EM-2'nin M50'si); açık yazım kod incelemesinin konusu (İddia A).
29. **RFC 2047'nin 76 karakterlik kodlanmış sözcük sınırı uygulanmaz;** yalnız RFC 5322'nin 998
    sekizlik satır sınırı uygulanır (§4). Sabit ASCII konular kodlanmadığı için bugün etkisi
    yok; ASCII dışı uzun bir konu bazı okuyucularda farklı görünebilir (ölçülmedi).
30. **Gerçek SES'e karşı davranış ölçülmedi** (genel sınır; 3, 4, 5'in kapsadığından geniş):
    EM-2'nin bütün ölçümleri test içi sahte sunucuya karşıdır — EM-5'in canlı dumanı ölçer.
31. **"0 dial" sahte sunucunun kabul ettiği bağlantı sayısıyla ölçülür;** ürün kodunda bir dial
    kancası yoktur (İddia C'nin ölçümü bu dolaylı sayıya dayanır).
32. **Fuzz testi başlık üreticisini sürer, ağ yolunu değil** (İddia C).
33. **Gövde içeriği kısıtlanmaz:** `Text`/`HTML` yalnız boş olmama ve geçerli UTF-8 için
    denetlenir; içerik kuralları şablonların işidir (§8, İddia H) — sayılı sınır 22 ile birlikte
    okunur.
34. **Kesik 250 kabul edilir** (4. tur, güvenlik D1; §2, §3): veri sonu yanıtında `250 ` kodu
    okunduktan sonra satır tavan, süre dolması ya da iptalle kesilirse gönderim **başarılı**
    sayılır ve `message_id` boş döner. Kök neden `bufio.Reader.ReadLine`'ın kısmi satırı
    hatasız döndürmesidir (B38) ve QUIT'in yanıtı beklenmez. Bedeli: kesik yanıtın gövdesi
    hiç okunmaz; röle 250'yi gönderdiyse doğru yön budur (sınır 15'in tersi değil, tamamlayıcısı).
    Pinli: `TestSend_ACutReplyToTheEndOfDataIsAcceptance` — `"250 Ok "` + 300 KiB, CRLF yok →
    başarı, boş `MessageID`, ≤ 2 s içinde döner; kesik 250'yi `network` sayan mutasyon (M66)
    kırmızı.
35. **`classify`'da süre ile tavanın sırası pinsizdir** (§3): süre/iptal kontrolünü tavan
    kontrolünün **önüne** alan bir değişikliği (M15) kırmızıya çevirecek bir test, tavan
    hatasıyla biten bir okumayı **aynı anda** bitmiş bir bağlamla birlikte gerektirir — bu
    zamanlanamayan bir yarıştır. Sıra koddadır ve kod incelemesinin konusudur. (El sıkışmada
    tavan → `network` ayrıca pinlidir — §3.)

## Karar verilmedi

- **Audit'te alıcının anahtarlı özeti (HMAC).** Ne kazandırır: `invite.code_emailed` ve EM-6
  satırına alıcı adresinin HMAC'ı yazılırsa, soruşturma *"kod hangi adrese gitti, adres sonra
  neye çevrildi"* sorusunu adresi saklamadan, aday bir adresi özetleyip karşılaştırarak
  cevaplayabilir (sayılı sınır 2 ve 19'u daraltır). Neden bu turda **uygulanmıyor**: (1) yeni
  bir anahtar ister — üretim, Secret'a ekleme, diğer anahtarlardan ayrılık denetimi
  (`config.go` `keySeparation` emsali), döndürme ve döndürmede eski özetlerin anlamı; (2) GDPR:
  bir adresin anahtarlı özeti **kişisel veridir** (takma adlandırılmış), anahtar elde olduğu
  sürece bilinen bir adresle eşleştirilebilir, ve Q13'ün silme akışı `employees` üzerinde bir
  UPDATE'tir — **değişmez** `audit_log`'a ulaşmaz (B29'un gerekçesi); (3) `audit_log` değişmez
  olduğu için yanlış bir tasarım geri alınamaz. Kararın sahibi: EM-7 ya da ayrı bir ADR, Q13 ile
  birlikte.
- **Kuyruk boyunun kesin değeri** — başlangıç 32 ve gerekçesi §6.2'de; kesin değer EM-5'in
  ölçümüyle. İşçi sayısı, geri çekilme süresi, boşaltma süresinin kesin değeri ve oran
  sınırlarının kesin sayıları — EM-5/EM-7, aritmetikle (§9'un girdileri).
- Sıfırlama için alıcı başına gönderim tavanı (sayılı sınır 6).
- Davet gönderim hatasının audit eylem adı (EM-7).
- Devre kesici açıkken sıfırlama grant'ının sonucu (senkron `undelivered` önerilir; EM-5).
- *"X via Taptime"* gönderen adı (VIES koşulu, WL-11) ve e-postada logo — ADR 0023.
- DMARC politikasının `none` → `quarantine` → `reject` takvimi — işletim kararı (dış adım 3).

## Sonuçlar

- **EM-2…EM-9 bu ADR'yi normatif kaynak alır;** EM-10, EM-11, EM-12 kendi ADR'leriyle gelir.
  Güvenlik denetimi (EM-2, EM-5, EM-7) bu ADR'nin on iddiasını (A–J) listesine alır.
- **EM-2'nin sapmaları** (a: `invalid_message` · b: AUTH 4xx → `throttled` + tek deneme · c:
  `ErrNotConfigured` · d: kendi `Message-ID`'miz · e: QUIT'in 221'i beklenmez · f: başlık reddi
  C1, U+2028/U+2029 ve `=?` ile genişledi · g: `Ref` iletilmez ve biçimli · h: ayrı görünen ad
  kontrolü yok · i: süre iki yoldan) orkestratörce kabul edildi ve §2–§4'e işlendi.
- **3. turda orkestratörün iki kuralı, EM-2 sapması olarak kayıtlı:** **j** — sunucudan okunan
  bayta deneme başına **256 KiB** tavan, `NewClient`'ten önce takılan tek sayaç, aşımda
  `network` (§2, §3, İddia J) · **k** — `message_id` yankı kuralı **8 karakterlik paylaşılan
  pencere**, değer listesinde `Text`/`HTML`, kullanıcı adı, parola ve tel üstündeki AUTH PLAIN
  argümanı (§2, İddia B, sayılı sınır 17 ve 21). **Eşitleme turunda EM-2'nin teslim ettiği koda
  eşitlendi** (sınıf `network`; liste: ham ileti, `To`, `From`, `Subject`, `Ref`, `Text`, `HTML`,
  kullanıcı adı, parola, AUTH argümanı; kısa id birebir alt dizgeyle de düşer).
- **EM-2'nin 2. tur kararları** (bu ADR'ye işlendi; eşitleme turunda EM-2'nin koduyla
  doğrulandı — `Credential` tipi, `Message` redaksiyonu, ağ hatasının yeniden denenmemesi ve
  duraklamada `throttled`'ın gerekçesi de eklendi):
  alıcıda `=?` reddi (§4) · STARTTLS'e verilen her yanıt kodu, başarısız el sıkışma ve STARTTLS
  sonrası EHLO hatası → `tls_unavailable`, tekrar yok (§3) · `Reply-To` `New`'de alıcı kuralıyla
  (§4, §5) · kullanıcı adı ve parola `mail.Credential` (§5, İddia D).
- **EM-2'ye devir (belge) — kapandı:** `Message`'ın belge yorumu *"beyan edilmiş istisna"*
  cümlesini ve ⚠️ *"loglanmaz"* uyarısını taşıyor (`internal/mail/mail.go`).
- **EM-2'nin kapanış teslimi ADR'ye işlendi:** kesik 250'nin kabulü
  (`TestSend_ACutReplyToTheEndOfDataIsAcceptance`, sınır 34) · bölünmüş ve el sıkışma tavan
  vakaları (`TestSend_TheReplyCapCoversTheWholeAttempt`, İddia J) · adım adım bağlantı sayısı
  (`TestSend_ClassifiesEachStep`, §2) · `Message` için `slog.AnyValue(m).Resolve()`, `%x`, `%d`
  (`TestMessage_PrintsAsAPlaceholder`, İddia D) · yankı kaçışının ölçümü
  (`TestMessageID_KnownLimitIsACutOrReorderedEcho`, sınır 17) · süre/tavan sırası sayılı sınır
  35 olarak kaldı (zamanlanamayan yarış).
- **EM-3'e devir (pinler, §2):** `mail.Config{RootCAs: …}` ve `c.RootCAs = …` kaynak pini
  (yalnız `mail.Config`), config'te CA anahtarı 0, manifestte `SSL_CERT_FILE`/`SSL_CERT_DIR`
  env'i 0.
- **EM-4 / EM-7 / WL-11'e devir (tasarım kararı, §8):** tenant adının e-postadaki biçimi —
  URL benzeri adın DKIM imzalı oltalamaya dönüşmesi (sayılı sınır 22). **EM-4 kapattığı
  yarı:** ad kapısı (izin listesi, gizleme) — §8'in EM-4 notu. **Açık kalan:** adın yalnız
  doğrulanmış tenant'ta gösterilmesi ya da riskin açıkça kabulü — **EM-7 `email` modunu açmadan
  önce kullanıcıya sorulacak ürün kararı** (sayılı sınır 22); sıfırlama e-postasında ad (EM-5;
  bugün yok).
- **EM-5'e devir (§6):** tampon 32 + ölçüm, koşullu panik kuralı, §6.6(b)'nin T ≥ D'li
  davranış testi (başlama sırası `go`'dan önce kaydedilir ya da assert edilmez).
- **Q02 cevaplandı** — orkestratör `open-questions.md`'de "Cevaplananlar"a taşır (bu ADR o
  dosyaya dokunmaz).
- **Bu görevde eklenen notlar:** ADR 0005 §5'e 2026-10-03 ek notu (Y-D daralır; kendi sayılı
  sınırlarıyla) · ADR 0015 durum notu · M7-04 ve M7-07 kart notları.
- **Bilinçli güncellenecek mevcut testler:** `TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed`
  (`"Q02"` beklentisi — EM-3) · gönderimi senkron varsayan sıfırlama testleri — okundu, en az
  `TestAdminReset_DeliveryGoesToTheAddressOnTheRow` ve `TestAdminReset_AFailedDeliveryIsRecordedAndNotShown`
  kanalı ve izi yanıt döner dönmez okur (tam liste EM-5'te ölçülür) · bir kayıt ya da panel ekranı
  e-posta vaat ederse `TestSignupSurface_UsesNoUnanchoredCapabilityWord`'ün sözlüğü
  (`"we will email"`, `"we email"` çapalı bir iddia ister) · kapanış bütçesi testleri (EM-5
  yuvalanma ve davranış testlerini ekler — §6.6).
- **Orkestratöre — CLAUDE.md güncellemesi gerekecek:** §3 dizin haritasına `internal/mail`
  satırı; §7'nin *"asla loglanmaz"* listesine sıfırlama token'ı, SMTP kimlik bilgisi ve
  e-posta adresi. Bu ADR CLAUDE.md'ye dokunmaz.
- **Orkestratöre — m10 dış adımlarına ek:** SES geri bildirim yönlendirmesi kapalı ya da bildirim
  adresi operatörün kutusu (§11) · yerel SES denemesi için sandbox'ta kalan, kısıtlı, ayrı bir IAM
  kimliği; prod kimliği `.env`'e yazılmaz (§12).
- **Yeni bağımlılık yok:** `net/smtp`, `net/mail`, `net/textproto`, `mime`, `mime/multipart`,
  `mime/quotedprintable`, `crypto/tls`, `crypto/x509`, `crypto/rand` — stdlib.
