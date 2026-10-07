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
  aşar. Volume yolu (c)'nin dışındadır (İddia I, *Yakalamadığı*). *(EM-3 2. ve 3. tur: mount
  yolu ayrıca pinlendi — uygulama manifestinde hiç mount yok; sayılı sınır 23 ve EM-3 notu.)*
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
   EM-5'te. *(EM-5A, 2026-10-03: 32 kesinleşti — ölçüm ve gerekçe aşağıdaki "EM-5A notu".)*
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
   değiştirmez. Süreç ölümü sayılı sınır 1'dir. *(EM-5A 2. tur, 2026-10-03: kural artık
   **eşzamanlı yazıcılar altında da** tutar — satırları işçi ve her isteğin yedek yolu birlikte
   yazar, bütçe `httpx.Limiter.TryCharge` ile tek kilitte okunup yazılır: bütçe kadar satır ve
   tam bir `rate_limited`. Ayrıntı ve ölçüm *"EM-5A notu"*nda.)*
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
     daha. **İki ardışık düzen vardır ve süre assert'i yalnız birini görür (EM-5A 2. tur
     düzeltmesi):** boşaltmayı Shutdown'dan **sonra** koşan düzen en az T + D sürer ve her
     pay > 0 onu eşiğin üstünde bırakır; boşaltmayı Shutdown'dan **önce** koşan düzen ise
     uçuştaki istek boşaltma **sırasında** koştuğu için ≈max(T, D) sürer ve süre assert'inden
     **geçer** (üçüncü göz ölçtü: 5/5 yeşil, 2,000–2,002 s). Onu ikinci bir assert ayırır:
     dizi çağrıldıktan sonra dinleyici kısa bir sınır içinde yeni bağlantıyı **reddeder**
     (Shutdown ilk iş dinleyicileri kapatır; önce-boşaltan düzende dinleyici D boyunca açık
     kalır). Örnek: T = D = 1 s, pay = 300 ms → eşik 1,7 s; doğru uygulama ≤ ≈1,55 s,
     sonra-boşaltan ≥ 2 s, önce-boşaltan ≈1 s ama dinleyici ≈1 s açık.
   - **Neden uçuşta istek şart:** uçuşta istek yokken Shutdown ≈0 s sürer ve 0 + D = max(0, D);
     doğru uygulama, işlev içi `Sleep(D)` ve tamamen ardışık uygulama üçü de ≈D ölçülür (B34).
     Denetçinin ölçümü (B34): T = D = 1 s'de doğru 1,056 s, işlev içi mutasyon 2,089 s, ardışık
     2,141 s — o koşularda pay 500 ms ile eşik 1,5 s üçünü ayırdı, ama D − pay = 500 ms yukarıdaki
     koşulu sağlamaz; B37 aynı şeklin kırmızı-yanlış verdiğini gösterdi.
   - **Başlama sırası:** assert edilecekse boşaltmanın başladığı an `go`'dan **önce**, eşzamanlı
     olarak kaydedilir; `go f()`'in içinde kaydedilen bir anla kurulan *"Shutdown dönmeden
     başladı"* assert'i doğru uygulamada da kırmızıdır (B34: 2000 denemenin 1896'sı). Ya da
     sıra assert'i hiç kullanılmaz. **Süre assert'i ardışıklığın yalnız bir yönünü ayırır**
     (Shutdown'dan sonra boşaltma); öteki yön (Shutdown'dan önce boşaltma) dinleyicinin hemen
     kapandığını ölçen assert'le ayrılır — o, başlama anını değil Shutdown'ın **etkisini**
     ölçtüğü için B34'ün yarışına düşmez (EM-5A 2. tur).
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
*(EM-9, 2026-10-06: bildirim uygulandı — tek URL'si `TAPPA_BASE_URL` + `/admin/login`, eşitlikle
denetlenir; gönderim yolu ve kararlar aşağıdaki "EM-9 notu".)*

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
  sıfırlama, bildirim). *(EM-9: kesici hâlâ yok; bildirimin kendi hesap başına tavanı
  "EM-9 notu"nda.)*
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
  dedi ve dev'den SES sandbox'ına bilinçli bir deneme engellenmemeli. (**EM-3 eki,
  2026-10-03:** §12 ve sayılı sınır 25, EM-5/EM-7 kanalları bağlandıktan sonraki durumu
  anlatır; o güne dek ikili, kanalı olmayan `email` modunu **her ortamda** reddeder —
  `cmd/tappa`'nın `unbuiltDelivery`'si. Yapılandırma (`config.Load`) dev'de reddetmez.
  **EM-5A, 2026-10-03:** sıfırlama yarısı kalktı — ikili `TAPPA_RESET_DELIVERY=email` ile
  her ortamda açılır; reddedilen yalnız davet akışının `email`'idir, EM-7'ye dek.)
  Bedeli, sayılı:
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
- **EM-5A (2026-10-03):** ölçüldü — PART I/II test adları, girdileri ve mutasyonlarıyla
  *"EM-5A notu"*nda (İddia E).
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
- **EM-5A (2026-10-03):** ölçüldü — PART I/II test adları, girdileri ve mutasyonlarıyla
  *"EM-5A notu"*nda (İddia F).
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
- **Tehdit modeli (EM-3'te eklendi; manifest ve Dockerfile pinleri için):** "Bu pinler manifestlere ve Dockerfile'a kazara giren sapmaya karşıdır; pini atlatmak için bilerek yazılmış bir manifest ya da imaj kod incelemesinin konusudur."
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
  biçimiyle); manifestte kök konumunu değiştiren env. **Yakalamadığı:** değiştirilmiş bir imaj
  — Go ataması da env de olmadan kök havuzunu değiştirir (B32; sayılı sınır 23; kök konumlarına
  bağlanan bir volume EM-3'ten beri pinli — EM-3 notu); `TAPPA_SMTP_HOST`'u değiştirebilen
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
   sıfırlama ve davet e-postası durur *(EM-9, 3. tur: ve "parolanız değişti" bildirimi de —
   aynı hesap ve aynı röle; bildirim o pencerede `undelivered` yazılır, "EM-9 notu" sınır 11)*. m10 dış adım 11'in alarmları (`BounceRate > 0.02`,
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
    okur. **Pinli (EM-3, fail-closed — tablolarındaki biçimlerle):** üst düzey manifestlerde ve
    Dockerfile'da `SSL_CERT` kelimesi — kaçış, satır devamı, büyük/küçük harf ve kelimenin
    **tamamını** tutan bir `ARG` dahil (§2'nin (c)'si, `TestPackaging_NothingMovesTheSystemRoots`)
    — ve uygulama manifestinde **hiçbir bildirilmiş mount**
    (`TestPackaging_NoMountShadowsTheSystemRoots`); `/etc/ssl` ya da `/etc/pki` altına,
    kendisine ya da atasına bir volume bunun içindedir. **Pinsiz** — kod ve manifest
    incelemesinin konusu: kök deposu değiştirilmiş bir imaj ve **Dockerfile'a eklenen bir `COPY
    … /etc/ssl/certs/` satırı** (kurumsal bir CA'yı imaja koymak — gerçekçi, kazara bir sapma,
    pinden geçer, R11); masum parçalardan kurulan bir ad (`ARG P=SSL_` + `ENV ${P}CERT_FILE`,
    E01) ve `!!` taşımayan açık bir YAML etiketi (`!<tag:yaml.org,2002:binary>`, E02);
    **taranan dosyaların dışındaki kanallar** — bir kustomize patch'i (R06b), alt dizindeki bir
    kustomization (R06c; tarama yalnız `deploy/k8s/*.yaml`), Helm `extraEnv` gibi bir değer
    şablonu (R07b), `deploy.yml`'e eklenecek bir `kubectl set env`, canlı `tappa-config`'e elle
    eklenmiş bir anahtar (`kubectl apply`'ın üçlü birleştirmesi elle eklenen anahtarı
    silmeyebilir — ölçülmedi); ve pinleri atlatmak için **bilerek** yazılmış her manifest ya
    da imaj (EM-3 notunun tehdit modeli).
24. **`TAPPA_SMTP_HOST` için izinli ad listesi yok:** ConfigMap'i değiştirebilen biri röleyi,
    herkese açık güvenilir sertifikası olan kendi sunucusuna çevirebilir; TLS doğrulaması geçer
    ve SMTP kimliği ile her link o sunucuya gider. Bunu yapabilen yetki bugün Secret'ı da
    okuyabiliyorsa yeni bir yetki değildir — ConfigMap ve Secret yetkilerinin kümede aynı
    olduğu **ölçülmedi**.
25. **Dev'de `email` modu boot'ta reddedilmez** (§12, orkestratör kararı; EM-5/EM-7 kanalları
    bağlandıktan sonraki durum — o güne dek ikili `email`'i her ortamda reddeder, §12'nin EM-3
    eki; **EM-5A'dan beri sıfırlama akışı için canlı**): dev'de açılırsa hedef
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
  sınırlarının kesin sayıları — EM-5/EM-7, aritmetikle (§9'un girdileri). *(EM-5A: kuyruk
  boyu 32, işçi 1, boşaltma 3 s / yazım payı 1 s — kararlaştı, "EM-5A notu". Oran
  sınırlarının sayıları açık.)*
- Sıfırlama için alıcı başına gönderim tavanı (sayılı sınır 6). *(EM-5A 4. tur: karar ya da
  açık kullanıcı risk kabulü EM-5B'nin önkoşulu oldu — "EM-5A notu", sayılı sınır 13; §9 devre
  kesicisi bu kararın yerini tutmaz.)* *(EM-9, 2026-10-06: "parolanız değişti" bildirimi aynı
  anahtarla açılır ve önkoşul onu da kapsar; bildirime **hesap başına** bir tavan konuldu,
  alıcı başına tavan yine yok — "EM-9 notu", sınır 3.)*
- Davet gönderim hatasının audit eylem adı (EM-7).
- Devre kesici açıkken sıfırlama grant'ının sonucu (senkron `undelivered` önerilir; EM-5).
  *(EM-9, 3. tur: bu öneri "parolanız değişti" bildirimine **uygulanmamalı** — kesici onu sayabilir
  ama reddetmemeli ya da bildirimin ayrı bir bütçesi olmalı; "EM-9 notu" sınır 11.)*
  *(EM-5A: kesici bu görevde kurulmadı — sapma ve devir "EM-5A notu"nda; karar açık.)*
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
  env'i 0. — **kapandı** (aşağıda *"EM-3 notu"*).
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

## EM-3 notu — 2026-10-03 (uygulama; normatif içerik değişmedi)

EM-3 §5'i ve §2'nin üç pinini uyguladı. Taban dal ucu `b06370c`; DB'ye bağlanılmadı, kümeye
dokunulmadı (`kubectl` yok, `tappa-secrets`'a hiçbir fiil yok).

- **Yazıldı:** `internal/config` — `TAPPA_RESET_DELIVERY` `none` | `email`,
  `TAPPA_INVITE_DELIVERY` `panel` | `email` (ikisi de kapalı küme; boş = kapalı; büyük harf ve
  çevreleyen boşluk affedilir; ret mesajı kümeyi ve bu ADR'yi adlandırır, **değeri tekrar
  etmez**), `Config.InviteDelivery`, `Config.Mail` (`mail.Config`; kullanıcı adı ve parola
  `mail.NewCredential` ile **yüklenirken** sarılır), `SMTPCredentialVariables()` ·
  `cmd/tappa/main.go` — `unbuiltDelivery`: `email` her iki akışta da açılışı **veritabanı
  aranmadan** durdurur (EM-5 / EM-7'ye dek) · `deploy/k8s/05-config.yaml` (iki akış
  `none`/`panel`, host `email-smtp.eu-central-1.amazonaws.com`, port 587, `Taptime
  <no-reply@taptime.mt>`, `TAPPA_MAIL_REPLY_TO` **boş**) · `deploy/k8s/20-app.yaml` (iki
  `optional: true` `secretKeyRef`) · `deploy/README.md` *"Transactional e-mail (M10 EM-3)"*
  runbook'u · `deploy/examples/` iki dosyasında yorumlu iki girdi. `cfg.Mail` ile hiçbir
  gönderici kurulmaz; `mail.New` yalnız sonda yapılandırmasıyla doğrulama için çağrılır
  (`mailRule`, `config.Load` sırasında) ve bağlantı açmaz (gönderici kurmak EM-5/EM-7'nin).
- **Döngü yok, ölçüldü:** `go list -deps ./internal/mail` → depodan yalnız kendisi;
  `go list -deps ./internal/config` → depodan `internal/mail`, `internal/policy`, kendisi. §5'in
  *"config sarar"* yolu uygulandı; OP-7 emsaline dönülmedi.
- **`TestLoad_ResetDeliveryIsAClosedSetAndFailsClosed`'un `"Q02"` beklentisi bilerek
  değişti:** artık `TAPPA_RESET_DELIVERY`, `"none"`, `"email"` ve `ADR 0022`'yi arar ve
  değerin tekrar edilmediğini ister (§5 *"Bayatlayan metin"*). Dokunulan dosyalarda Q02'yi
  *"cevapsız"* diye anan metin düzeltildi: `config.go`, `main.go`, `05-config.yaml`,
  `deploy/README.md` (kabul edilmiş sınırlar 3. madde). Dokunulmayan dosyalardaki anışlar
  (`internal/invite`, `internal/adminauth`, `internal/handler`, uygulanmış migration
  yorumları) sayılı sınır 13'te kalır.

**Kararlar (gerekçeli):**
1. **Kural tek kaynaktan:** `From`, `Reply-To` ve iki kimlik bilgisi için config kuralı
   kopyalamaz, `mail.New`'e sorar (`mailRule`: `mail.New`'in kabul ettiği sabit bir sonda
   yapılandırmasında yalnız sınanan alanı değiştirir, ret değişkenin adıyla sarılır; bağlantı
   açılmaz). Pin: `TestLoad_SenderAddressesFollowTheTransportsOwnRule` her satırı hem
   `config.Load`'a hem `mail.New`'e sorar; ikisi **tablodaki 21 satırdan birinde**
   ayrışırsa kırmızı — yalnız orada: üçüncü gözün X06'sı (kuralı hiçbir satırın ulaşmadığı
   uzunluktaki bir From için atlayan değişiklik) yeşil kaldı. Tablo bir örneklemdir.
2. **Üretimde host kuralı §5'ten bir adım dar** (ama loopback adları üzerinde tam değil —
   sınırlarda): `localhost`, `*.localhost` ve
   `netip.ParseAddr`'ın kabul ettiği IP literal'e ek olarak **son etiketi rakamla başlayan**
   ad da reddedilir — C çözücülerinin IPv4 diye okuduğu yazımlar (`127.1`, `0x7f000001`);
   hiçbir üst düzey alan adı rakamla başlamaz. Ve **her ortamda tek yazım:** küçük harfli DNS
   adı (`isDNSHostName`, `TAPPA_OPERATOR_HOST`'un kuralı) ya da IP literal — `LOCALHOST` ve
   `localhost.` üretim kuralına hiç ulaşmaz (tek yazım büyük harfi ve sondaki noktayı
   kapatır, başka loopback adlarını değil). Geliştirmede loopback ve IP izinli (§12).
3. **Yalnız boşluk = eksik** (operatör DSN'i emsali): `mail.New` boşluktan oluşan bir parolayı
   kabul ederdi.
4. **`email` config'te geçerli, `cmd/tappa`'da reddedilir:** `main.go` `InviteDelivery`'yi hiç
   okumuyordu; ret olmasa davet akışında `email` **sessiz bir varsayılan** olurdu (kod yine
   yöneticinin ekranında). §12 ve sayılı sınır 25, EM-5/EM-7 kanalları bağlandıktan sonraki
   durumu anlatır; o güne dek ikili, kanalı olmayan `email` modunu **her ortamda** reddeder
   (`unbuiltDelivery`) — §12'ye aynı cümle eklendi. Yapılandırmanın (`config.Load`) dev'de
   `email`'i reddetmemesi korunur.
5. **(c) Dockerfile'ı da tarar:** imajın `ENV`'i de hizmet konteynerinin ortamıdır — §2'nin
   *"manifest"*inden geniş, gerekçeli.
6. **`TAPPA_MAIL_REPLY_TO` ConfigMap'te boş:** karar verilmiş bir yanıt kutusu yok; boş =
   başlık yok.
7. **Yalnız 465 reddedilir** (§5'in yazdığı); SES'in ikinci örtük TLS portu 2465 kabul edilir
   (sayılı sınır, aşağıda).

**Tehdit modeli (manifest ve Dockerfile pinleri; 3. tur):** "Bu pinler manifestlere ve Dockerfile'a kazara giren sapmaya karşıdır; pini atlatmak için bilerek yazılmış bir manifest ya da imaj kod incelemesinin konusudur."
Bu yüzden pinler **dar anlamda fail-closed**'dur: YAML ya da Dockerfile sözdizimini
ayrıştırmazlar, metni bir ayrıştırıcıdan **geniş** okurlar (ucuz olduğu yerde yorumlar dahil);
meşru bir gelecek ihtiyacı (bir mount, bir ters bölü, bir etiket) testi kırmızıya çevirir ve o
değişiklik pini bilerek günceller. Reddettikleri **tablolarındaki biçimlerdir**, fazlası değil:
masum parçalardan kurulan bir ad (E01), `!!` taşımayan açık bir etiket (E02) ve imaja COPY
edilen bir kök dosyası (R11) geçer — tablolarda *KNOWN LIMIT* satırı olarak ölçülüdür.

**İddia I'nın PART II'si artık adlarıyla:** fail-closed matris
`TestLoad_MailSettingsFailClosedWhenAFlowIsEmail` (3 akış biçimi × 4 zorunlu anahtar ×
{gerçekten yok (`os.Unsetenv`), boş, yalnız boşluk} = 36 vaka, ayrıca tek geçişte dördü) ·
iki akış kapalıyken okunmaz
`TestLoad_MailSettingsAreNotReadWhileBothFlowsAreOff` · 465 her ortamda
`TestLoad_SMTPPortRefusesImplicitTLSEverywhere` · üretimde loopback/IP
`TestLoad_SMTPHostRefusesLoopbackAndAddressesInProduction` (2. tur: `::`, `::ffff:7f00:1`,
`::ffff:127.0.0.1` satırları) · hata metninde değer yok
`TestLoad_MailErrorsNameTheVariableNeverTheValue` — iki kimlikte **her 3 baytlık pencere**
aranır (3. tur; nöbetçiler bunun için kurgulandı — kapanış denetçisinin N12/N12b'si), öteki
değerlerde yalnız **önek** (tamamı ve ilk 8, 4 ya da 3 karakteri; 2. tur, X07) · alan eşlemesi (kimlikler yer
değiştirmemiş, `RootCAs` nil) `TestLoad_MailConfigCarriesTheEnvironmentsValues` · §2 (a)
`TestMailConfig_NoProductCodeSetsTheRootPool` (go/types, dışa aktarım verisiyle; kontrolü
`TestRootPoolUses_SeesEveryWrittenForm`) · (b) `TestPackaging_ConfigNamesNoCertificateVariable`
(yüklemi `TestCertificateVariable`) · (c) `TestPackaging_NothingMovesTheSystemRoots` (kuralı
`TestRootPinFindings`) · mount `TestPackaging_NoMountShadowsTheSystemRoots` (kuralı
`TestMountFindings`) ·
manifest `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest`,
`TestPackaging_TheSMTPCredentialsAreOptionalSecretKeys`,
`TestPackaging_TheConfigMapShipsTodaysDelivery`,
`TestPackaging_TheConfigMapsMailSettingsLoadInProduction` · ikili
`TestArtifact_RefusesAnEmailDeliveryThisBuildLacks`, `TestUnbuiltDelivery_RefusesEmailForEitherFlow`.
**İddia D'nin EM-3 vakası:** `TestLoad_MailCredentialsAreRedactedInTheConfig` (`Config` ve
`Mail` üzerinde altı `fmt` fiili, slog metin/JSON, `encoding/json`; değer de ilk 8 karakteri de
yok).

**2. tur (2026-10-03 — güvenlik ONAY 1 ORTA + 2 DÜŞÜK, üçüncü göz RED; yalnız test ve metin):**
- **(a) aynı biçimli struct (güvenlik ORTA, üçüncü göz X14):** `mail.Config`'inkiyle aynı
  alanları taşıyan başka bir struct `mail.Config`'e dönüşür, yani `s := shape(c); s.RootCAs = p;
  return mail.Config(s)`, `(*shape)(c).RootCAs = p` ve `mail.Config(struct{…}{RootCAs: p})`
  `mail.Config`'in alanını **adlandırmadan** havuzu kurar — üçü de yeşildi. Taramaya iki kural
  eklendi: (1) `RootCAs` adlı her alan seçicisi ya da bileşik değer anahtarı, alanı **tanımlayan**
  struct'ın tipi `mail.Config`'in struct'ıyla `types.Identical` ise; (2) hedefi `mail.Config` ya
  da `*mail.Config` olan ve kaynağı başka bir tip olan her dönüşüm — `unsafe.Pointer`'dan
  `*mail.Config`'e dönüşüm dahil. Kontrol örneği üç biçimi, `unsafe.Pointer` dönüşümünü ve
  dönüştürülmeden kurulan aynı biçimli bir literali taşır — üç kuralın her biri için yalnız onun
  yakaladığı bir satır; başka biçimli bir struct'ın `RootCAs` alanı ve `mail.Config`'in kendine
  dönüşümü raporlanmaz. Turun mutasyonlarında 1. turun M38'i (eski kuralda anahtarların
  atlanması) artık **eşdeğerdir**: aynı satırları yeni anahtar kuralı yakalar.
  **(a)'nın yakalamadığı (güncel):** `reflect`; `*mail.Config`'e dönüştürmeden alanın belleğine
  yazan `unsafe` aritmetiği (`unsafe.Add`, bir ofset); `internal/mail` içinde kurulup dışarı
  verilen bütün bir `mail.Config` değeri (bugün yok).
- **Kök konumlarına mount (güvenlik DÜŞÜK):** sayılı sınır 23 yalnız *"`/etc/ssl/certs`
  üzerine volume"* diyordu. Go Linux'ta (go1.27.1 `crypto/x509/root_linux.go`, okundu)
  `certFiles`'ın — `/etc/ssl` ve `/etc/pki` altında altı demet yolu — yalnız **var olan
  ilkini** okur (*"stop after finding one"*) ve iki `certDirectories`'in — `/etc/ssl/certs` ve
  `/etc/pki/tls/certs` — **her** dosyasını okur. Yani ilk demeti değiştiren bir mount ya da bu
  iki dizine `subPath` ile eklenen tek bir PEM yeter; bir ata dizine (`/etc/ssl`, `/etc`, `/`)
  mount da öyle. *"Hiç mount yok"* bu yollardan **geniştir** — güvenli yönde. 2. turda mount'lar ayrıştırılıp yolları karşılaştırılıyordu; **3.
  turda fail-closed** (kapanış denetçisinin M-flow'u — `volumeMounts` satırındaki akış dizisi —
  ayrıştırıcıyı geçti): `deploy/k8s/20-app.yaml`'ın ham metni, yorumlar dahil, boşluk ve ters
  bölüler atılıp küçük harfe çevrildiğinde `volumemounts` de `mountpath` de **içermez**
  (`TestPackaging_NoMountShadowsTheSystemRoots`, kuralı `mountFindings` / `TestMountFindings`).
  Bugün öyle. İlk meşru mount testi kırmızıya çevirir ve yol kuralını o değişiklik yazar; bütün
  dosya okunduğu için init konteynerinin mount'u da sayılır (bilerek fazla yaklaşım).
- **(c) yazımları (üçüncü göz X16; 3. turda fail-closed):** kural tek işlevdir,
  `rootPinFindings` (tablosu `TestRootPinFindings` — kapanış denetçisinin N14b'si ters bölü
  yasağını kaldırınca paket yeşil kalmıştı): (1) **her dosyada** (manifestler + Dockerfile) ham
  metin, **yorumlar dahil**, boşluk ve ters bölüler atılıp küçük harfe çevrildiğinde
  `ssl_cert` içermez — YAML kaçışı, ters bölüden sonra boşluklu Dockerfile satır devamı (D-cont-ws,
  denetçi ölçtü: Docker `SSL_CERT_FILE` kurar), katlanmış skaler, büyük/küçük harf ve
  kelimenin **tamamını** (`SSL_CERT` ya da `SSL_CERT_`) tutan bir `ARG` (D-arg) bunun içindedir;
  kelimeyi masum parçalara bölen bir `ARG` (`ARG P=SSL_` + `ENV ${P}CERT_FILE`, E01) pinsizdir —
  kuralın sıkıştırmaya dayandığını kelimeyi
  `SSL_CERT`'ün **içinden** bölen tablo satırları ölçer (kapanış turunda eklendi; onlarsız
  sıkıştırmayı kaldıran mutasyon yeşil kalıyordu); yorum ayıklanmadığı için tırnaklı `#` hilesi
  (C-hash, C-hash-sq, D-hash) kendiliğinden kapanır; (2) **manifestlerde**, yalnız `#` ile
  başlayan satırlar dışında, ters bölü yok (`\x53SL_CERT_FILE` gibi bir kaçış — sıkıştırma
  onu birleştirmez, ters bölü kuralı okur) ve `!!` etiketi yok (`!!binary`, `!!str` — C-binary);
  `!!` taşımayan açık etiket (`!<tag:yaml.org,2002:binary>`, E02) pinsizdir. Bugün hepsi 0.
  **Bedeli, sayılı:** ileride meşru bir ters bölü (ör. bir ingress annotation regex'i) ya da
  etiket; **hizmet dışı** bir manifestte (ör. migrate Job'ı ya da Postgres) ya da Dockerfile'ın
  **build** aşamasında meşru bir `SSL_CERT` (R14, R15 — kural dosyanın tamamını okur, aşamayı ya
  da kaynağın kime gittiğini bilmez); `SSL_CERT` anan bir yorum — her biri testi kırmızıya
  çevirir ve o değişiklik pini günceller.
- **4. tur (kapanış denetimi):** `TestRootPinFindings`'e denetçinin tanıkları satır olarak
  eklendi — sekmeli ve CR'li satır devamı (S04), `#` taşıyan satırda kaçış ve sonda yorumlu
  `!!binary` (S08), akış eşlemesinde tırnaklı `#`'ten sonra kaçış (S08b), alt çizgisiz
  `ARG P=SSL_CERT` (S09), `\u` ve `\U` kaçışları (S11), `!!str` (S10) — ve üç *KNOWN LIMIT*
  satırı (E01, E02, R11; beklenen bulgu 0). Taranan küme kendi testi olan bir işlevdir
  (`rootPinFiles` / `TestRootPinFiles`: Dockerfile, `20-app.yaml`, `05-config.yaml` ve ≥ 8
  manifest; döngü taranan dosyaları sayar) — W01/W04 kırmızı. `TestMountFindings`'e iki bütün
  pod satırı (yalnız birinde mount). **Sayılı sınır:** mount pininin test gövdesi kurala hangi
  metni verdiğinin kendi kâhinidir — çağrı yerinde dosyayı dilimleyen bir değişiklik (W02, W03)
  yakalanmaz; kod incelemesinin konusu.
- **(b) sözcük listesi (üçüncü göz X15):** `CAFILE`, `CAPATH` (OpenSSL `-CAfile`/`-CApath`,
  curl `--capath`), `CABUNDLE`, `TRUST`, `TRUSTSTORE` ve `CERT` **içeren** her parça eklendi.
  **Liste kapalı değildir:** listede olmayan sözcüklerle adlandırılmış bir değişken (`…_POOL`,
  `…_ISSUER`) geçer; onun `mail.Config`'e ulaşmasını (a) yakalar. Ve tarama yalnız
  `internal/config`'in dosyalarını okur — başka bir paketin doğrudan `os.Getenv`'i (b)'nin
  dışındadır.
- **Davet kümesi (X23):** davet akışının kötü değer listesine `e-mail` eklendi.

**EM-3'ün sayılı sınırları (bu ADR'nin 23 ve 24'üne ek):**
- Üretim host kuralı yalnız `localhost`, `*.localhost` ve IP adresini (her yazımıyla)
  reddeder ve **yazıma** bakar, çözümlemeye değil: `ip6-localhost`, `localhost.localdomain`
  ve loopback'e çözülen her DNS adı üretimde **kabul edilir** (üçüncü gözün B7'si). Tek
  etiketli bir ad (`relay`) da kabul edilir; küme içinde arama listesiyle bir servise
  çözülebilir (TLS'i herkese açık bir sertifikayla geçmesi ise beklenmez — ölçülmedi).
- "Değer yok" ölçüsü: iki kimlikte her 3 baytlık pencere aranır (1–2 baytlık sızıntı
  ölçülmez); öteki değerlerde yalnız **önek** — tamamı ve ilk 8, 4 ya da 3 karakteri; bir
  sonekin ya da iç parçanın sızması orada ölçülmez.
- Yorum ayıklayıcıya (`stripYAMLComments`, tırnaklı `#`'te keser) dayanan pinler kalır: iki
  kimliğin `optional: true` pini ve ConfigMap değer pinleri — tehdit modelinin
  *"bilerek yazılmış"* kısmı; kök konumu ve mount pinleri artık ona dayanmaz.
- 2465 (SES belgesi, ölçülmedi) kabul edilir: o portta her gönderim süre dolana dek bekler,
  kimlik gitmez.
- Akışlar kapalıyken ayarlar okunmaz: ConfigMap'teki bozuk bir değer açılışta değil CI'da
  (`TestPackaging_TheConfigMapsMailSettingsLoadInProduction`), Secret'taki bozuk bir kimlik ise
  ancak akış açıldığında görünür.
- `Config`'in kalanı (ham anahtarlar) redakte edilmez — backlog T80; yalnız iki SMTP kimliği
  kendi tipiyle redakte.

**Devirler:** EM-5 — `case email` → `mail.New(cfg.Mail)` + `unbuiltDelivery`'den sıfırlama
satırının çıkması + `TestUnbuiltDelivery_RefusesEmailForEitherFlow` ve
`TestArtifact_RefusesAnEmailDeliveryThisBuildLacks`'in o satırının güncellenmesi;
`TestPackaging_TheConfigMapShipsTodaysDelivery` ancak akışı açan deploy kararıyla değişir.
EM-7 — davet satırı için aynısı.

## EM-5A notu — 2026-10-03 (uygulama; normatif içerik değişmedi — kararlar, sapmalar ve ölçümler aşağıda)

EM-5 iki kısma bölündü (orkestratör kararı, 2026-10-03). **EM-5A** (bu not): kod, test içi bir
SMTP sahtesine karşı ölçülen her kabul ve `unbuiltDelivery`'nin sıfırlama yarısının kalkması.
**EM-5B** (kullanıcının SES dış adımlarına bağlı): M7-04'ün sahteye karşı koşmuş kriterlerinin
**gerçek SMTP'ye** (SES) karşı yeniden koşusu ve canlı duman (Gmail *Show original*: SPF, DKIM,
DMARC PASS; message-id; kendi `Message-ID`'miz; izleme kapalı). EM-K9'a uygun: repoya posta
servisi ya da compose eklenmedi; sahte, `internal/handler/fakesmtp_test.go`'da test içi bir
`net.Listener`'dır (EM-2'nin `internal/mail` emsali), sertifikası her koşuda bellekte üretilir.
Taban dal ucu `6d31015`. **ConfigMap değişmedi** (`none` / `panel`), yani sevk edilen davranış
bugün değişmez; açmak bir deploy kararıdır (§12). DDL yok, migration yok; DB testleri dev
Postgres'e test tenant'ı ve audit satırı yazar (öteki DB testleri gibi). `go.mod`/`go.sum`/
`sqlc.yaml` diff boş.

**Yazıldı:**
- `internal/handler/adminresetoutbox.go` (yeni) — kuyruk (`resetOutbox`: tampon 32, **tek**
  işçi), `dispatch` (bloklamadan sunum; dolu ya da kapanıyorsa senkron `undelivered`), `work`,
  `handle` + `contain` (grant başına `recover`), `deliverJob`, `Drain` (iki fazlı boşaltma),
  `sendErrorClass`; sabitler `resetSendGrace` 15 s, `resetAuditGrace` 5 s, `ResetDrainGrace`
  3 s, `ResetDrainWriteReserve` 1 s; `undelivered` satırının beş sabit nedeni.
- `internal/handler/resetmail.go` (yeni) — `emailResetChannel`, `NewEmailResetChannel`.
- `internal/handler/adminreset.go` — `Request` grant'ları `dispatch` eder; `deliver(base, ip,
  g, decided)` gönderim ve satır için **iki ayrı bağlam**; hata satırına `reset_id`, `class`,
  `smtp_code`; `recordOutcome`; `ResetDelivery.ResetID`; defter yorumları güncellendi.
- `cmd/tappa/main.go` — `case config.ResetDeliveryEmail` → `mail.New(cfg.Mail)` +
  `handler.NewEmailResetChannel(…)`; `NewAdminReset` işçiyi başlatır; kapanış dizisi
  `shutdown(srv, httpGrace, outbox, drainGrace)` işlevine taşındı (eşzamanlı);
  `unbuiltDelivery`'nin sıfırlama satırı çıktı — davet satırı EM-7'ye dek kalır.
- Metin eşitlemesi (davranışı tarif eden cümleler): `internal/config/config.go`,
  `web/templates/email/email.go`, `internal/handler/adminresetlimits.go`
  (`resetRequestFloor`'un ardılı artık var), `deploy/k8s/05-config.yaml` (yorum; değerler
  aynı), `deploy/README.md` (üç yer).

**Senkron varsayan ve güncellenen testler (tam liste — POST'tan sonra kanalı ya da izi okuyan
her test, okunarak):** `TestAdminReset_RegisteredAndUnregisteredAreByteIdentical`,
`TestAdminReset_NoResponseCarriesTheLink`, `TestAdminReset_DeliveryGoesToTheAddressOnTheRow`,
`TestAdminReset_AFailedDeliveryIsRecordedAndNotShown`,
`TestAdminReset_ACompletedRecoveryIsNeverSilenced`,
`TestAdminReset_ARefusedLinkIsBudgetedOnTheLinkAndNotOnTheAccount` (ayrıca bütçenin gerçekten
tükendiğini önce doğruluyor), `TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText` (biçim
başına bir akış — kanal artık işçinin), `TestPanelRecoveryDB_EndToEnd`,
`TestPanelRecoveryDB_AnUnregisteredAddressIsIndistinguishable` — her biri okumadan önce kuyruğu
boşaltır (üretimdeki `Drain`). `TestUnbuiltDelivery_RefusesEmailForEitherFlow` ve
`TestArtifact_RefusesAnEmailDeliveryThisBuildLacks`'in sıfırlama satırı bilerek tersine döndü
(EM-3 devri kapandı). `TestPackaging_TheConfigMapShipsTodaysDelivery` değişmedi (yalnız hata
cümlesi).

**Kararlar (gerekçeli):**
1. **Tek işçi** (§6.3 *"1–2"*): boşaltmada en çok **bir** gönderim uçuştadır, yani sayılı
   sınır 15'in boşaltma penceresi tek linktir; **kuyruğa alınmış** grant'ların satırları
   kuyruğun aldığı sırayla yazılır (reddedilen — dolu ya da kapanan kuyruk — grant'ın satırını
   kendi isteği yazar; o satırlar işçininkilerle her sırada karışabilir). Bedel
   §6.2'nin aritmetiğidir: en kötü gönderim süresinde 10 dakikada 40 grant, tampon tek adresle
   dolabilir.
2. **Tampon 32 kesinleşti:** dolu bir tamponun yazım maliyeti ölçüldü — tek uçuşta + 32
   bekleyen = **33** `undelivered` satırı, gönderimler kesildikten sonra gerçek audit
   kaydedicisiyle dev Postgres'e yapıcının ölçümünde **116–184 ms**'de (`-race`, altı koşu, yük
   ortalaması ≈9), üçüncü gözün ölçümünde yük altında **150–294 ms**'de yazıldı
   (`TestResetOutboxDB_AFullOutboxFitsTheWriteReserve`). Yazım payı **1 s**: ölçülen aralığın
   **≈3,4–8,6 katı**.
3. **Boşaltma bütçesi D = 3 s, iki faz:** gönderim D − 1 s'ye (2 s) dek sürer; sonra uçuştaki
   gönderim iptal edilir ve kalan her grant gönderilmeden `undelivered` alır — yazımlar bütçenin
   içinde. D dolarsa yazılmakta olan satır bırakılır ve `Drain` hata döner (havuzun kapanışı
   Kubernetes'in öldürmesinden sonraya itilmesin diye).
4. **Eşzamanlılık `srv.RegisterOnShutdown` ile değil**, `shutdown()` içinde `Shutdown`'dan
   **önce** açılan bir goroutine ile (§6.6'nın *"ör."*'ü bir öneriydi): dizi testin sürdüğü
   işlevin içinde kalır, başlama anı `go`'dan önce bellidir. Sıra assert'i yerine **iki**
   assert (2. turda düzeltildi — üçüncü gözün X03'ü): dizi çağrıldıktan sonra **≤ 300 ms**
   içinde dinleyici yeni bağlantıyı **reddeder** (Shutdown'dan ÖNCE ve ardışık boşaltan düzeni
   ayırır; ölçülen ≈0,2 ms, X03'te ≈2,0 s) ve toplam süre **< T + D' − pay** (Shutdown'dan
   SONRA boşaltan düzeni ayırır). Yalnız süre assert'i X03'ü göremiyordu: uçuştaki istek
   boşaltma sırasında koştuğu için o düzen de ≈max(T, D') sürer (§6.6 *"Başlama sırası"*).
5. **Audit'e ayrı bağlam** (§6.3): taban `context.WithoutCancel(istekCtx)` (istek kimliği
   korunur, iptal düşer); gönderim = taban + `resetSendGrace` + boşaltmanın kesmesi; işçinin
   satırı = taban + `resetAuditGrace` + boşaltmanın tüm bütçesi. **Kuyruk-dolu/kapanıyor
   yedeğinin satırı** (istekte yazılır) = taban + `resetAuditGrace`, boşaltma bütçesine
   **bağlı değil**: kapanışta uçuştaki bir istek HTTP boşaltmasının içindedir (20 s), havuz
   açıktır; satırı kuyruğun tükenen bütçesine bağlamak onu düşürürdü (bu turda bulundu ve
   düzeltildi — M36).
   **Bu turun öz denetiminde bulunan iki yarış daha, düzeltildi:** (a) `Drain`'in ilk hâli
   bütçe dolunca gönderimi durdurma ve satırları bırakmayı `ctx`'e bağlı bir
   `context.AfterFunc`'a bırakıyordu ve `defer`'deki `stop` iptal yayılımıyla yarışıp
   **kazanabiliyordu** (`cancelCtx.cancel` önce `Done`'u kapatır, sonra çocukları iptal eder) —
   ikisi de hiç yapılmazdı; şimdi `ctx.Done()` dalında eşzamanlı çağrılıyor (M38 kırmızı).
   (b) `context.AfterFunc` zaten bitmiş bir bağlamda bile işlevini **ayrı bir goroutine'de**
   koşturur, yani durdurucu bittikten sonra kurulan bir gönderim ya da satır bağlamı bir an
   canlı kalıyordu; `boundedBy` artık bunu dönmeden bitiriyor (M37 kırmızı; M36 bu kontrol
   yokken **yeşil** kalmıştı).
6. **Panik kuralı "satır yazılmadıysa" → "satırın yazımına başlanmadıysa":** `decided` bayrağı
   satırın yazımına girilmeden hemen önce kurulur. Audit yazıcısının **kendi içindeki** panik
   0 ya da 1 satır bırakır (bilinmez) — ikinci satır yazılmaz (sayılı sınır 3, aşağıda).
7. **`ValidFor` = `ExpiresAt − now` (gönderim anı), `ResetTTL` değil** (EM-4 devri): kuyruk bir
   grant'ı bekletebilir ve e-posta linkin kalan ömründen fazlasını vaat etmemeli; tipik metin
   *"59 minutes"*. Bir dakikanın altı render'da reddedilir → `undelivered`, röle aranmaz.
8. **Birden çok hesaba çözülen adres:** e-postaya **ad eklenmedi** (§8, EM-4 notu); hesap başına
   bir e-posta (en çok `MaxCandidates` = 8), linkleri dışında birbirine benzer.
9. **Log (§10):** başarı satırı (kanal) `reset_id`, `message_id`; hata satırı (`deliver`) `ip`,
   `admin_user_id`, `reset_id`, `err_type` ve — hata `*mail.SendError` ise — `class`,
   `smtp_code`; kuyruk reddi `ip`, `admin_user_id`, `reset_id`, `queue`; panik satırı
   `admin_user_id`, `reset_id` (değer yok). **Bir grant ya da bütçe hakkında ölçülen sekiz
   satır** — gönderim hatasının iki dalı (düz hata ve kanalın her röle reddinde döndürdüğü
   `*mail.SendError`), hesap ve link bütçesi satırları, başarısız audit yazımı, panik, kuyruk
   reddi ve e-posta kanalının başarı satırı — isteğin değerlerini taşıyan bir bağlamla yazılan
   `*Context` çağrısıdır, yani httpx'in sarmalayıcısı isteğin `request_id`'sini ekler (2. tur:
   `audit write failed` ve bütçe satırları `h.log.Error`/`Warn` idi, `*Context`'e çevrildi; 3.
   tur: `SendError` dalı, başarı satırı ve link bütçesi satırı teste eklendi —
   `TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID`). Akışın başka bir şey hakkındaki
   satırları (istek yolunun kendi retleri) ölçülmedi — adlarıyla sayılı sınır 16.
10. **`ResetDelivery.ResetID`** (opak UUID) eklendi — `mail.Message.Ref` ve kanalın başarı
    satırı için; `Ref` iletilmez (§2).
11. **`undelivered` satırının `reason`'ı** nedene göre beş sabit cümle: gönderim hatası · kuyruk
    dolu · kuyruk kapanıyor · boşaltma bitti · işçi hatası.
12. **`NewEmailResetChannel`** `TAPPA_BASE_URL`'i bir sonda render'ıyla açılışta sınar (kural
    e-posta paketinindir — `https`, düz `http` yalnız loopback); geçmeyen taban açılışı durdurur.
13. **`unbuiltDelivery`'nin sıfırlama yarısı kalktı** — §12'nin EM-3 eki tarihlendi; sayılı
    sınır 25 sıfırlama akışı için canlı.
14. **Bütçeler tek adım: `httpx.Limiter.TryCharge`** (2. tur, orkestratör kararı — yapısal
    düzeltme). Satırı işçi de, kuyruğun reddettiği grant için her istek de yazar, yani bir
    yöneticinin bütçesine N yazıcı aynı anda dokunur. *"`Allowed`, sonra `Charge`"* iki kilitti:
    üçüncü göz 40 eşzamanlı yedek satırda 300 koşunun **244**'ünde bütçe aşımı (en çok **24**
    satır / bütçe 10) ve **63**'ünde `rate_limited` satırının **hiç** yazılmadığını ölçtü (senkron
    tabanda da vardı, gerileme değil). `TryCharge` okuma ve yazmayı tek kilitte yapar:
    `n := sayaç+1; within := n ≤ limit`; sınırı geçen tek şarj (`n == limit+1`) **reddedilen**
    bir çağırana düşer ve tek `rate_limited` satırını o yazar. Sırayla eski çiftle **aynı**
    kararı verir (`TestLimiter_TryChargeDecidesAsAllowedThenChargeDid`, pencere dönümü dahil).
    Sıfırlama akışının **beş** bütçesinin hepsi geçirildi: istek, gönderim, hesap (audit), link
    (audit) ve süreç-log (`unknown`) — sonuncusunda red de şarj olur, pencere içinde hiçbir
    kararı değiştirmez. Hesap ve link bütçesi yarış testleriyle, istek, gönderim ve süreç-log
    bütçeleri (3. tur) `TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers` ile pinli.

**Sapmalar (açıkça):**
- **§10'a `queue` anahtarı eklendi** — değeri yalnız `full` | `closing` (sabit); kuyruk
  reddinin nedenini log'da ayırır, adres, link ya da metin taşıyamaz. Kapalı küme bu ekle
  birlikte, kuyruk satırının **kendisine ulaşan** testte pinli:
  `TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds` (dolu ve kapanan kuyruk; 1. turda
  anahtar taraması bu satıra hiç ulaşmıyordu — üçüncü gözün X19/X19b'si).
- **§9'un devre kesicisi bu görevde yok.** Kesici her gönderimi (davet dahil) sayan paylaşılan
  bir nesnedir; EM-5A'nın kabul listesinde değildi ve davetin kanalı olmadan yarısı kurulur.
  *Karar verilmedi*'deki *"devre kesici açıkken sıfırlama grant'ının sonucu"* bu yüzden açık —
  **devir:** EM-7 (ya da ayrı kart), önerilen sonuç senkron `undelivered`.
- §6.6'nın önerdiği `RegisterOnShutdown` yerine açık goroutine (karar 4).

**Güvenlik iddiaları — üç parçalı (EM-5A).** Tehdit modeli: *"Bu pinler bu dosyalara,
çağıranlarına ve `cmd/tappa`'nın kapanış dizisine kazara giren sapmaya karşıdır; bir pini
atlatmak için bilerek yazılmış kod, kod incelemesinin konusudur."* Mutasyonlar
kopyala-değiştir-geri yaz yöntemiyle, her birinden sonra dosyanın sha256'sı doğrulanarak
koşuldu (M-numaraları bu notun ve kartın tablosu).

*İddia E (EM-5A) — sıfırlama yanıtının süresi gönderime bağlı değildir.*
- **PART I:** `TestAdminReset_TimingIsFlatWhileTheRelayTakesTwoSeconds` — kanal 2 s, kayıtlı
  kolda 40 ms basım, 5 + 5 iç içe örnek; assert: iki medyan [250, 300] ms; ölçülen (`-race`, altı
  koşu) 250,3–251,8 ms; kontrol: senkron yol (`h.deliver`) ≥ 2 s, bandın dışında.
  `TestEmailResetChannel_TimingIsFlatWithASlowRelay` — gerçek `mail.SMTP`, röle veri sonu
  yanıtını 2 s tutuyor; 250,2–251,0 ms. Bozan: M01 (istek içinde senkron gönderim) — kayıtlı
  medyan 2,04 s ve 2,00 s, iki test KIRMIZI.
- **PART II:** iki zamanlama testi; yakaladıkları: M01. **Yakalamadığı:** istek yolunda 50 ms'den
  kısa bir bekleme; iki kolu birlikte yavaşlatan bir değişiklik. **İddianın kapsamı yanıttır**
  (durum, gövde, yanıt süresi — 4. turda daraltıldı): grant'ın **kuyruktaki akıbeti** üzerinden
  numaralandırma — gönderim anı (`Date`), e-postadaki kalan süre (`ValidFor`), kuyruk doluyken
  istekçinin kendi tenant'ına düşen `undelivered` — iddianın **dışındadır**, ölçülmedi (bu notun
  sayılı sınırı 12); kuyruk doluyken yedek satırın senkron yazımının gerçek Postgres'teki süresi
  de (sınır 14).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia F (EM-5A) — süreç ölmediği (ADR ana listesinin sayılı sınırı 1), hesap bütçesi
aşılmadığı, audit yazıcısı panik etmediği (bu notun sayılı sınırı 3: 0 ya da 1 satır, bilinmez),
audit yazımı başarısız olmadığı (başarısız bir yazım 0 satır bırakır ve `audit write failed`
olarak loglanır, yeniden denenmez) ve boşaltmanın bütçesi tükenmediği (bu notun sayılı sınırı 6)
sürece her grant tam bir satırla biter; bütçe eşzamanlı yazıcılar altında da tamdır; boşaltma
HTTP boşaltmasıyla eşzamanlı ve bütçesinin içindedir.*
- **PART I:** `TestResetOutbox_EveryGrantInsideTheBudgetEndsInExactlyOneRow` — sekiz alt test (bütçe **içindeki** her grant; bütçe dışındaki grant'ın kendi satırı yoktur, son alt test bunu da sayar): gönderildi (bir
  adresin arkasında üç hesap → üç `requested`) · gönderim hatası · kuyruk dolu (bir uçuşta + 32
  bekleyen + bir fazla → fazlası `undelivered`, gönderilmez) · kuyruk kapalı (boşaltma başladıktan
  sonra sunulan iki grant → iki `undelivered`, 0 gönderim) · satırdan önce panik → `undelivered`
  · satırdan **sonra** panik → ikinci satır yok · boşaltma (bir uçuşta + iki bekleyen; iz, bitmiş
  bağlamdaki yazımı Postgres gibi reddeder → üçü de `undelivered`, röle tek çağrı görür) · bir
  yöneticinin 12 başarısız grant'ı → bütçenin 10 satırı + bir `rate_limited`.
  `TestAdminReset_AFullOutboxRecordsTheGrantAtOnceAndDoesNotWait` — kuyruk doluyken yanıt
  ≤ taban + 50 ms, taşan grant'ın satırı yanıt dönerken var, gönderilmez.
  `TestResetOutbox_APanickingSendBecomesUndeliveredAndTheWorkerLives` — değeri alıcıyı ve linki
  taşıyan panik → `undelivered`, ikisi de log'da yok, aynı işçi sonraki grant'ı teslim eder.
  `TestResetOutbox_TheRequestEndingDoesNotCancelTheSend` — gönderim uçuştayken istek bağlamı
  iptal → gönderim tamamlanır, `requested`.
  `TestResetOutbox_ConcurrentRequestsForOneAccountStayInsideItsBudget` — gerçek router'dan tek
  yönetici için yirmi adresten aynı anda yirmi istek, **üç durumda**: kuyruk boş (satırları
  işçi yazar), kapalı (her istek kendi satırını yazar), dolu (işçi bir grant'ı tutarken her
  istek yazar) → hepsine aynı sayfa ve **tam** 10 satır + bir `rate_limited` (2. tur; 1. turda
  yalnız boş kuyruğu ölçüyordu). `TestResetOutbox_RacingRowWritersStayInsideTheBudget` —
  üçüncü gözün ölçümü test olarak: bir yönetici için 40 eşzamanlı yedek satır (100 koşu), işçi
  ile yedeğin aynı bütçeye 60 grant'la yarışması (30 koşu), bir linkin 40 eşzamanlı reddi (100
  koşu) → her koşuda tam bütçe + bir `rate_limited`; ilkel düzeyde
  `TestLimiter_TryChargeIsExactUnderConcurrency` (200 eşzamanlı şarj, 50 koşu → tam 10
  `within`, sınırı geçen tek şarj reddedilene). `TestResetOutbox_AClientThatLeavesStillGetsItsFallbackRow`
  — kuyruk kapanırken ya da doluyken ziyaretçi gider (istek bağlamı biter) → yedek satır yine
  yazılır (iz bitmiş bağlamı reddeder). `TestResetOutbox_ASecondFaultLeavesTheWorkerAlive` —
  aynı grant'ta gönderim ve audit yazıcısı birlikte panik eder → işçi yaşar, sonraki grant
  `requested`. `TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID` (3. turda genişledi)
  — ölçülen sekiz satırın her biri isteğin kimliğini taşır: gönderim hatası (düz hata ve
  `*mail.SendError` — bu satırın anahtarları ayrıca §10'un kapalı kümesinde, `class=rejected`,
  `smtp_code=550`), hesap bütçesi, link bütçesi (11 farklı adresten bir linkin 11 tekrarı),
  başarısız audit yazımı, panik, kuyruk reddi ve e-posta kanalının başarı satırı (test içi
  röleyle gerçek `mail.SMTP`). Başka satırlar ölçülmedi (sayılı sınır 16).
  `TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers` (3. tur) — tek adresten aynı
  anda 100 istek, 100 gönderim ve bir linkin 160 sunumu (her biri 200 koşu; istekler başlama
  çizgisinden önce kurulur, taban bekleme no-op) → tam 20 yanıt + 80 × 429 + bir uyarı, tam 10
  `Consume` + 90 × 429 + bir uyarı, tam 60 süreç-log satırı.
  `TestResetOutbox_AnOfferRacingTheDrainNeverSendsOnAClosedQueue` — 200 kez 30 sunum bir
  `Drain`'le yarışır, kapalı kuyruğa gönderim olmaz (`-race`).
  `TestResetOutbox_ASpentBudgetAbandonsTheRowBeingWritten` — bütçe bir satır yazılırken dolar →
  `Drain` hata döner, işçi 1 s içinde biter, satır bırakılır ve loglanır; **ardından** gelen bir
  isteğin yedek satırı yine yazılır (bağlamı isteğinkidir).
  `TestResetOutboxDB_AFullOutboxFitsTheWriteReserve` — karar 2'nin ölçümü.
  `TestShutdown_DrainsTheResetOutboxAlongsideTheHTTPServer` (§6.6(b)) — T = 2 s süren istek
  uçuşta, röleye takılı bir gönderim ve iki bekleyen grant; D' = `ResetDrainGrace` −
  `ResetDrainWriteReserve` = 2 s, pay 600 ms, eşik T + D' − pay = **3,4 s**; **ve** dizi
  çağrıldıktan sonra **≤ 300 ms** içinde dinleyici yeni bağlantıyı reddeder (2. tur). Ölçülen
  (beş koşu, `-race`, 2. tur): ret **≈0,18–0,21 ms**, toplam **2,05–2,12 s**; üç grant'ın her
  biri tek `undelivered`, röle tek çağrı, uçuştaki istek 200. Pay'ın koşulu: D' − pay = 1,4 s
  > ≈550 ms (B35). `TestShutdownBudget_TheResetDrainNestsInsideTheHTTPGrace`
  (§6.6(a)). `TestArtifact_BootsAndStopsWithTheResetEmailChannel` — sevk edilen ikili `email`
  ile açılır, form teslim edebilir hâlde, SIGTERM'de 0 ile çıkar, kimlik baytı basmaz.
- **PART II — yakaladıkları:** satır testi — reddedilen sunumun satırsız kalması (M03), panik
  kuralının koşulsuz uygulanması (M06), satırdan önceki panikte satır yok (M07), `decided`'in
  satırdan sonra kurulması (M08), boşaltmanın gönderim durdurmasının yok sayılması (M09),
  uçuştaki gönderimin kesilmemesi (M10b), `Drain`'in kuyruğu kapatmaması (M12),
  `recordOutcome`'un bütçeyi atlaması (M24), satırın gönderimin iptal edilmiş bağlamıyla
  yazılması (M26), ikinci bir işçi (X02 — tam-kuyruk ve satır testleriyle; denetçinin koşusunda kapanış testi de), kapanan
  kuyruğun "dolu" diye yazılması (M32), boşaltma başladıktan sonra kuyruğun grant alması (M33:
  kapalı kanala gönderim, test ikilisi ölür) · eşzamanlılık ve yarış testleri —
  `recordOutcome`'un bütçeyi atlaması (M34), `dispatch`'ın yedek satırının bütçeyi atlaması
  (X12), `TryCharge`'ın yeniden "`Allowed`, sonra `Charge`"a bölünmesi (M39; ilkel testi M39h) ·
  ziyaretçi-gider testi — yedek satırın isteğin kendi bağlamına bağlanması (X10) · ikinci-hata
  testi — yedek satır yazımının `contain` dışına çıkması (X15: test ikilisi ölür) · istek-kimliği
  testi — işin bağlamının isteğin değerlerini düşürmesi (X13); bağlamsız yazılan `audit write
  failed` (M40), hesap bütçesi satırı (M41), `SendError` dalının hata satırı (MY07a), kuyruk
  reddi satırı (MY07b), kanalın başarı satırı (MY07c), link bütçesi satırı (MY07d), düz hata
  dalının satırı (MY07e), panik satırı (MY07f) · kapılar testi — istek (MY04a), gönderim (MY04b)
  ya da süreç-log (MY04e) bütçesinin yeniden *"`Allowed`, sonra `Charge`"*a bölünmesi ·
  `Drain` yarışı testi — `offer`'ın `closed`'ı
  kilitsiz okuması (X14: kapalı kanala gönderim) ·
  kuyruk-dolu testi — bloklayan sunum (M02), sessiz düşürme (M03) · panik testi — yeniden
  fırlatma (M04: test ikilisi ölür), değerin loglanması (M05), satırsız panik (M07) · istek-sonu
  testi — işin isteğin iptalini taşıması (M25) · bütçe testi — yazımın bütçeyle bitmemesi (M27),
  isteğin yedek satırının kuyruğun tükenen bütçesine bağlanması (M36), `Drain`'in tükenen-bütçe
  dalının işçiyi durdurmaması (M38) · durdurucu testi
  (`TestResetOutbox_AnEndedStopperEndsTheContextAtOnce`) — `boundedBy`'ın eşzamanlı kontrolünün
  kalkması (M37) · DB payı
  testi — kesmenin olmaması (M11b) · kapanış testi — Shutdown'dan SONRA boşaltma (M13: 4,10 s,
  süre assert'i), Shutdown'dan ÖNCE ve ardışık boşaltma (X03: dinleyici ≈2,0 s açık kaldı,
  ret assert'i; 1. turda yeşildi), Shutdown'dan ÖNCE 400 ms bekleme (MY10: ret ≈400 ms, sınır
  300 ms), dizi içinde
  `Sleep(drainGrace)` (M14: 5,13 s), gönderim durdurmasının yok sayılması (M09b), uçuştakinin
  kesilmemesi (M10), kesmenin olmaması (M11) · yuvalanma testi — `ResetDrainGrace` >
  `httpShutdownGrace` (M15), payın bütçeye eşit olması (M16) ·
  `TestAdminResetConstants_ShippedValuesArePinned` — kuyruk boyu (M28) · ikili testi — `run()`'ın
  kanalı nil bırakması (M30); `TestUnbuiltDelivery_RefusesEmailForEitherFlow` +
  `TestArtifact_RefusesAnEmailDeliveryThisBuildLacks` — sıfırlama reddinin geri gelmesi (M29),
  davet reddinin kalkması (M31). **Yakalamadığı:** `run()`'da dizi işlevinin **dışına** eklenen
  bir bekleme; dizi içinde Shutdown'dan **ÖNCE** 300 ms'lik ret sınırından kısa bekleme (MY09,
  250 ms — **yeşil**; MY10, 400 ms — kırmızı); dizi içinde Shutdown başladıktan sonra pay'dan
  (600 ms) kısa ek bekleme (M14b, 200 ms — **yeşil**, ölçülmüş sınır); yalnız testin kurmadığı
  bir koşulda ödenen bekleme; `deliver`'daki `defer cancelSend()`'in kalkması (MY06 — **yeşil**;
  bu notun sayılı sınırı 11); süreç ölümü (ADR ana listesinin sayılı sınırı 1).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia B (EM-5A eki) — sıfırlama yolunun ölçülen log satırları (gönderim sonucu, kuyruk reddi —
dolu ve kapanan — ve hesap bütçesi satırı) adres, link, sunucu metni ve SMTP kimliği taşımaz;
anahtarları kapalı kümededir.*
- **PART I:** `TestEmailResetChannel_LogsOnlyIdsClassAndCode` — gerçek `mail.SMTP` + test içi
  röle, dokuz davranış: temiz 250 · id'si token'ı yansıtan 250 (`message_id` boş döner) · RCPT'de
  550 ve veri sonunda 554 (büyük harfli adres, link ve base64 link alıntılı) · iki kez 451 ·
  AUTH'a 535 · STARTTLS yok · selamlamayan röle · TLS el sıkışmasında takılan röle. Assert: tek
  satır sonucu; hata satırında `class`, `smtp_code`, `err_type=*mail.SendError`; her satırın
  **her anahtarı** §10'un kümesinde (bu ekle `queue` dahil); log'da adres (büyük/küçük harf
  duyarsız), link, token, base64 link, SMTP kullanıcı adı ve parolası (ve her birinin her 8
  karakterlik penceresi) ve rölenin sözleri **yok**; kontrol: rölenin yanıtı bu sırları
  gerçekten taşıyor. `TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText` (mevcut, işçiye
  taşındı) — `*mail.SendError` olmayan hata yolu. `TestEmailResetChannel_SendsEachLinkToTheRowsAddressAndNamesNobody`
  — üç hesap → üç ileti, her biri satırdaki adrese, kendi linkiyle iki parçada, telde reset id
  yok, ad yok. `TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds` (2. tur) — aynı anahtar ve
  değer taraması, `LogsOnlyIdsClassAndCode`'un ulaşmadığı üç satırda: kuyruk dolu, kuyruk
  kapanıyor ve hesap bütçesinin `rate limited` satırı (`scope` anahtarı o M7-04 satırının
  kendisinin); kontrol değerin **önekiyle** eşleşir, yani değere eklenen bir şey de taranır.
  `TestEmailResetChannel_AStoredAddressTheRelayCannotTakeIsNeverDialled` — satırda
  ASCII dışı bir adres → `invalid_address`, röle aranmaz, adres log'da yok.
- **PART II — yakaladıkları:** hata satırına hatanın kendisi (M17 — anahtar kümesi; M17b —
  `*mail.SendError` olmayan yol), `class`/`smtp_code`'un düşmesi (M18), başarı satırına alıcı
  (M19), `SendError`'ın sarılması (M20), panik değeri (M05). Ömür testleri — tam TTL (M21);
  kurucu testi — taban sınamasının (M22) ve nil göndericinin (M23) kalkması; teslim testi —
  alıcının iletiye yazılmaması (M35); kuyruk-ve-bütçe testi — kuyruk değerinde alıcı (X19),
  kuyruk satırına `to` anahtarı (X19b), bütçe satırına olay ayrıntısı (X20) — üçü de 1. turda
  yeşildi. **Yakalamadığı:**
  kümedeki bir anahtarın taşımaması gereken bir **değer** (küme adlar içindir; değer taraması
  yalnız listelenen sırlar içindir); tabloda olmayan bir röle davranışı; gerçek SES (EM-5B).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**EM-5A'nın sayılı sınırları (bu ADR'nin sınırlarına ek; M10 EM-5 kartındaki listeyle aynı):**
1. Gerçek röle (SES) ölçülmedi — EM-5B; bütün ölçümler test içi röleye karşı.
2. Tek işçi: kuyruk dolarken bir grant'ın gönderimi en kötü durumda dakikalarca (32 × 15 s)
   gecikebilir; e-postanın ömür ifadesi bunu yansıtır (karar 7), 1 dakikanın altı gönderilmez.
3. Audit yazıcısının **içindeki** panik 0 ya da 1 satır bırakır (karar 6).
4. **Bütçeler tek adımdır (karar 14); kalan, M7-04'ün bütçe sınırlarıdır:** sabit pencere
   (sınırda kısa bir patlama 2× limite ulaşır), süreç içi ve süreç başına sayaç, 100 000 anahtarda
   açık-yönlü unutma (`adminresetlimits.go`). *(1. turun bu maddesi *"iki goroutine … en çok bir
   satır"* diyordu — yanlıştı: yedek satırı her istek kendi goroutine'inde yazar, yazıcı sayısı
   N'dir; üçüncü göz 24 satır / bütçe 10 ölçtü. Belgelemek yerine yarış kaldırıldı.)* Aynı
   iki-adım deseni sıfırlama akışının **dışındaki** bütçelerde (giriş, aktivasyon, kayıt) hâlâ
   durur — bu görevin kapsamı dışında, sayıldı.
5. Kuyruk-dolu ve panik log satırlarının kendi bütçesi yok — hacimleri adres başına istek
   bütçesiyle sınırlı, dağıtık bir saldırganda sınırsız.
6. Boşaltma bütçesi biterse yazılmakta olan satırlar bırakılır ve loglanır (`audit write
   failed`); süreç çıkmaktadır — sınır 1'in kardeşi.
7. Kapanış testinin yakalamadıkları: `run()`'da dizi işlevinin dışına eklenen bekleme; dizi
   içinde Shutdown'dan ÖNCE 300 ms'lik ret sınırından kısa bekleme (MY09, 250 ms yeşil; MY10,
   400 ms kırmızı); Shutdown başladıktan sonra dizi içinde 600 ms'den kısa ek bekleme (M14b
   ölçüldü); testin kurmadığı bir koşulda ödenen bekleme (İddia F, *Yakalamadığı*).
8. Kayıtlı bir adrese gerçek ikiliden gönderim koşulmadı (yönetici fikstürü ve güvenilir bir röle
   gerekir); `main.go`'daki kablo ikilinin açılışı, formu ve SIGTERM'i ile pinli — EM-5B'nin canlı
   dumanı ölçer.
9. §9'un devre kesicisi yok (sapma, devir; EM-5B önkoşulu).
10. Zaman testleri 50 ms'lik banttadır; ölçülen medyanlar tabanın en çok 1,8 ms üstünde (`-race`,
    yük ortalaması 4–9, tam `internal/handler` `-race` koşusu dahil), kararsızlık görülmedi —
    daha ağır bir makinede ölçülmedi.
11. `deliver`'daki `defer cancelSend()` hiçbir testle pinli değil (MY06 — kaldırılması yeşil
    kaldı). Yalnız panik eden bir gönderimde iş görür: o olmadan o gönderimin zamanlayıcısı
    `resetSendGrace`'e (15 s), `sendsStopped` üzerindeki durdurma kancası boşaltmaya dek yaşar —
    panik başına tutulan bellek; kaybolan ya da yanlış bir satır değil (panik satırı ve
    `undelivered` satırı yine yazılır).
12. **Kuyruk-akıbeti numaralandırma kanalı** (ORTA, ölçülmedi; güvenlik denetimi, 4. tur).
    Kuyruk tek, FIFO ve tek işçilidir; bütün istekçiler onu paylaşır. Bir istekçinin **kendi**
    grant'ının akıbeti önündeki grant sayısına bağlıdır: gönderim anı (`Date` başlığı gönderimde
    yazılır — `internal/mail`), e-postadaki kalan süre (`ValidFor = ExpiresAt − now`, gönderim
    anında — `resetmail.go`) ve kuyruk doluyken kendi tenant'ına düşen `undelivered` satırı
    (`dispatch`). Önündeki grant sayısı önceki isteklerin kayıtlı bir adrese çözülüp
    çözülmediğine bağlıdır; kendi yönetici hesabı ve posta kutusu olan biri (kayıt herkese
    açık — herhangi bir müşteri) kayıt bilgisini **dolaylı** okuyabilir. İddia E yalnız yanıtı
    kapsar; `adminreset.go`'nun *"does not tell anyone whether an address is registered"*
    cümlesi yanıtla daraltıldı. **EM-5B önkoşulu** (Devirler).
13. **Alıcı başına gönderim tavanı yok** (ORTA; güvenlik denetimi, 4. tur). Hesap bütçesi audit
    **satırlarını** sınırlar, gönderimleri değil (satır gönderimden sonra yazılır; bütçe dışındaki
    grant yine gönderilir — `recordForAdmin`). Tek bir alıcıya giden hacmi yalnız **kaynak adres
    başına** istek bütçesi sınırlar (`adminResetRequestLimit`); tek işçi bütün gönderimleri
    birlikte sınırlar, bir alıcının payını değil; dağıtık bir kaynakta sınır yoktur. Planlanan §9
    devre kesicisi (EM-7) **süreç geneli** bir tavandır: tek alıcıda yoğunlaşan bir bombayı
    **durdurmaz**, tetiklendiğinde bütün tenant'ların sıfırlamasını durdurur — bombayı bir kurtarma
    kesintisine çevirir. ADR ana listesinin sınır 6'sının ve *Karar verilmedi*'deki maddenin bu
    akıştaki karşılığı. **EM-5B önkoşulu** (Devirler).
14. **Kuyruk doluyken yedek yolun senkron audit yazımı ölçülmedi** (DÜŞÜK). Kuyruk bir grant'ı
    reddedince satırı istek kendisi, `done()`'dan (tabanın bitişinden) **önce** yazar — ve yalnız
    kayıtlı adreste (kayıtsız adresin grant'ı yoktur), en çok `MaxCandidates` (8) grant için.
    Yazım tabanın (250 ms) altında kaldığı sürece fark yanıtta görünmez; gerçek Postgres'le, tam
    pencerede ve kuyruk doluyken **hiç ölçülmedi** (tam-kuyruk testi sahte izle ölçer).
15. **`ValidFor` saat kaymasında linkin gerçek ömründen uzun olabilir** (DÜŞÜK). `ExpiresAt`
    uygulamanın saatiyle basılır (`internal/adminauth/reset.go`, `r.now().Add(ttl)`), süre dolumunu
    ise veritabanının `now()`'ı uygular (`db/queries/passwordresets.sql`); e-postadaki süre
    gönderimde uygulamanın saatiyle hesaplanır. Veritabanının saati ilerideyse e-posta linkin
    kalan ömründen fazlasını yazar (fark kadar). Ölçülmedi.
16. **İstek yolunun kendi log satırları `request_id` taşımaz** (DÜŞÜK). `*Context` olmadan
    yazılırlar, yani httpx'in sarmalayıcısı kimliği ekleyemez; istek-kimliği testi bunları
    ölçmez (*"başka satırlar ölçülmedi"*, İddia F). Adlarıyla (`adminreset.go`): adres bütçesi
    *"panel recovery rate limited"* `scope=address` · *"panel recovery: issuing failed"* ·
    kayıtsız adresin *"panel recovery refused"* (`no active admin for that address`) ·
    `logUndeliverableAttempt`'in *"panel recovery refused"* (`a recovery link was presented…`) ·
    `beginPost`'un *"panel recovery refused: not same-origin"* ve *"… csrf mismatch"*; aynı
    biçimde yeni-parola yolunun satırları — *"panel recovery rate limited"* `scope=submit`,
    *"panel recovery completed"*, *"panel recovery: spending the link failed"*, *"panel recovery
    refused"* (`link did not resolve` ve `link resolved but could not be…`), yolun kendi
    origin/csrf satırları — iki *"minting the csrf value failed"* ve iki render satırı.

**EM-5A 2. tur (2026-10-03 — üçüncü göz RED, dört bloklayan; bulgu → değişiklik → ölçüm):**
- **B1** kapanış testi Shutdown'dan ÖNCE boşaltan ardışık düzeni (X03) göremiyordu → teste
  dinleyici-ret assert'i (≤ 300 ms) eklendi; §6.6'nın iki cümlesi ve karar 4 düzeltildi → X03
  KIRMIZI (dinleyici ≈2,0 s açık), M13 KIRMIZI kaldı; doğru düzende ret ≈0,2 ms.
- **B2** sayılı sınır 4 yanlıştı (yazıcı sayısı N) → yarış kaldırıldı: `httpx.Limiter.TryCharge`
  ve beş bütçe ona geçti (karar 14); yarış testleri eklendi → `TryCharge`'ı bölen mutasyon
  (M39) KIRMIZI, testler `-count=5 -race` yeşil.
- **B3** kuyruk-reddi satırı hiçbir taramaya girmiyordu → `TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds`
  → X19, X19b (ve denetçinin X20'si) KIRMIZI.
- **B4** `dispatch`'ın yedek satırının bütçeyi atlaması (X12) yeşildi → B2'nin testleriyle KIRMIZI;
  M24 cümlesi `recordOutcome` ile sınırlandı.
- **Bloklamayanlar:** N1 ikinci işçi *Yakalamadığı*'ndan çıktı (X02 kırmızı) · N2 ziyaretçi-gider
  testi (X10 kırmızı) · N3 `audit write failed` ve bütçe satırları `*Context`'e çevrildi, test
  eklendi (X13, M40, M41 kırmızı) · N4 `Drain` yarışı testi (X14 kırmızı) · N5 ikinci-hata testi
  (X15 kırmızı) · N6 karar 2'nin oranı ölçülen aralığa göre · N7 kartın tehdit modeli ve sınır
  listesi ADR'ninkiyle aynı · N8 satır sırası cümlesi daraltıldı · N9 EM-5B önkoşulu · N10
  `deliver`'da `defer cancelSend()` · N11 satır testinin adı ve İddia F başlığı (sınır 6
  dışarıda).

**EM-5A 3. tur (2026-10-06 — kapanış denetimi RED; yalnız test ve metin değişti, ürün kodunun
satırları 2. turunkiyle aynı):**
- **F1** (bloklayan) `SendError` dalının hata satırı istek kimliğinden düşse (MY07a) yeşil
  kalıyordu — testin kanalı yalnız düz hata döndürüyordu → istek-kimliği testine
  `&mail.SendError{Class: rejected, SMTPCode: 550}` döndüren vaka (satırın anahtarları ayrıca
  kapalı kümede) → MY07a KIRMIZI. Test `TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID`
  olarak yeniden adlandırıldı.
- **F2** İddia F başlığına sayılı sınır 3 ve başarısız audit yazımı koşulu eklendi.
- **F3** sayılı sınır 7 ve kodun PART II *"WHAT THEY DO NOT CATCH"*'ına Shutdown'dan ÖNCE 300 ms'den
  kısa bekleme eklendi (MY09 250 ms yeşil, MY10 400 ms kırmızı — ölçüldü).
- **F4** testin açılış cümlesinin *"her satır"*ı ölçülmeyen satırları da kapsıyordu → iki yol
  birden: başarı satırı (MY07c) ve link bütçesi satırı (MY07d) vakaları eklendi **ve** cümle
  ölçülen sekiz satıra daraltıldı (*"başka satırlar ölçülmedi"*). Düz hata dalı (MY07e) ve panik
  satırı (MY07f) için de mutasyon koşuldu → hepsi KIRMIZI.
- **F5** istek bütçesinin eşzamanlı aşımı (MY04a) hiçbir testte kırmızı değildi →
  `TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers` (istek, gönderim, süreç-log).
  İlk biçimi (40 çağıran, istekler goroutine içinde kuruluyor, gerçek 250 ms taban, 10 koşu)
  MY04a'yı **yeşil** bıraktı (ölçülen aşım oranı koşu başına ≈%2, 50 koşuda 1); istekler başlama çizgisinden önce
  kurulup taban no-op yapılınca koşu başına aşım `-race`'siz %7–48, `-race` ile %53–90 ölçüldü
  ve 200 koşuya çıkıldı → MY04a, MY04b, MY04e KIRMIZI. `Request`'teki *"concurrent requests can no
  longer all read allowed"* yorumu bu yüzden kaldı (pinli).
- **F6** EM-5B önkoşulundaki *"sayılı sınır 6"*nın ADR ana listesinin 6. maddesi (paylaşılan
  devre kesicisi, tek SES hesabı) olduğu yazıldı; İddia F'nin Yakalamadığı'ndaki *"süreç ölümü
  (sayılı sınır 1)"* da ana listeye bağlandı (bu notun 1. maddesi SES'tir).
- **F7** `defer cancelSend()`'in pinsiz olduğu (MY06 yeşil) sayılı sınır 11 oldu.
- **Öz denetim:** M03'ün (reddedilen sunum satır yazmaz) önceki turlardaki kırmızısı bir derleme
  hatasıydı, testi ölçmüyordu → mutasyon derlenir hâle getirildi → KIRMIZI, sebebi doğru (kapalı
  ve dolu kuyrukta grant'lar 0 satır). Tam koşuda (69 mutasyon) başka derleme hatası yok: 66
  KIRMIZI, 3 YEŞİL (M14b, MY09 — sayılı sınır 7; MY06 — sayılı sınır 11).

**EM-5A 4. tur (2026-10-06 — güvenlik denetimi ONAY; iki ORTA bulgu ConfigMap'i `email`'e
çevirmeyi blokluyor, üç DÜŞÜK; yalnız metin — `.go` dosyalarında yalnız `//` satırları değişti):**
- **ORTA — kuyruk-akıbeti numaralandırma kanalı** (ölçülmedi) → sayılı sınır 12; `adminreset.go`'nun
  *"does not tell anyone whether an address is registered"* cümlesi ölçülen kümeye (durum, gövde,
  yanıt süresi) daraltıldı ve kanalı adıyla anıyor; İddia E'nin kapsamı yanıt olarak yazıldı;
  `adminresetoutbox.go`'nun COUNTED LIMITS'i kanalı sayıyor; EM-5B önkoşulu (c).
- **ORTA — alıcı başına gönderim tavanı yok** → sayılı sınır 13; hesap bütçesinin satırları
  sınırladığı, gönderimleri değil (`recordForAdmin` yorumu) ve istek bütçesinin tek alıcıya
  giden hacmin tek sınırı olduğu (`adminResetRequestLimit` yorumu) yazıldı; EM-5B önkoşulu (b):
  devre kesicinin tek alıcılı bombayı durdurmadığı, tetiklenince bütün tenant'ların kurtarmasını
  durdurduğu açıkça.
- **DÜŞÜK** → sayılı sınır 14 (kuyruk doluyken yedek satırın senkron yazımı, yalnız kayıtlı
  adreste, gerçek DB ile ölçülmedi), 15 (`ValidFor` ve saat kayması), 16 (istek yolunun bağlamsız
  log satırları, adlarıyla).

**M7-04 kriterleri, test içi SMTP'ye karşı yeniden koşu:** tablo M10 EM-5 kartında (ölçüt
başına test adı ve sonuç). Gerçek SMTP'ye karşı yeniden koşu EM-5B'dir.

**Devirler:**
- **EM-5B:** M7-04 kriterlerinin gerçek SES'e karşı yeniden koşusu (tablo, kart) · canlı duman:
  Gmail *Show original* SPF=PASS (`mail.taptime.mt`), DKIM=PASS, DMARC=PASS; 250'de SES
  message-id'si ve yankı kuralından geçtiği (`message_id` log'da dolu); kendi `Message-ID`'mize
  ne olduğu; izlemenin kapalı olduğu (link `taptime.mt`'ye, piksel yok) · sayılı sınır 2, 3, 4,
  5, 9, 30'un SES ölçümleri · ConfigMap'i `email`'e çevirmek (deploy kararı;
  `TestPackaging_TheConfigMapShipsTodaysDelivery` bilerek güncellenir). **Önkoşullar —
  ConfigMap'i `email`'e çevirmeden ÖNCE üçü de karşılanır** (biri yetmez; her biri ayrı bir
  açığı kapatır):
  (a) **§9 devre kesicisi (EM-7) ya da açık bir kullanıcı risk kabulü** — kesici olmadan
  sıfırlama gönderimlerinin süreç geneli üst sınırı yoktur (ADR ana listesinin sayılı sınırı 6 —
  paylaşılan devre kesicisi ve tek SES hesabı; *bu notun* sayılı sınırı 6 değil — ve bu nottaki
  §9 sapması).
  (b) **Alıcı başına gönderim tavanı kararı ya da açık kullanıcı risk kabulü** (bu notun sayılı
  sınırı 13) *(EM-9: bildirimi de kapsar — EM-9 notu, sınır 3)*. (a) bunu **karşılamaz**: devre kesici süreç geneli bir tavandır, tek alıcıda
  yoğunlaşan bir bombayı durdurmaz; tetiklendiğinde bütün tenant'ların sıfırlamasını durdurur,
  yani bombayı bir kurtarma kesintisine çevirir.
  (c) **Kuyruk-akıbeti numaralandırma kanalı: bilinçli kullanıcı kabulü ya da grant akıbetinin
  istekçiye yansımasını azaltan bir tasarım** (bu notun sayılı sınırı 12).
- **EM-7:** davet kanalı + `unbuiltDelivery`'nin davet satırı; §9'un paylaşılan devre kesicisi
  ve *"kesici açıkken sıfırlama sonucu"* kararı (bu nottaki sapma).
- **EM-9:** `resetLetter` emsaliyle üçüncü mektup; gönderim yolu olarak bu kuyruk kullanılabilir.
  *(EM-9, 2026-10-06: üçüncü mektup yazıldı; kuyruk **kullanılmadı** — gerekçe aşağıdaki
  "EM-9 notu", karar 2.)*

## EM-6 notu — 2026-10-06 (uygulama; §7'den bir sapma — aşağıda)

Taban dal ucu `26e9ce0`. **E-posta gönderilmez;** davranış bugün panelde bir kart bölümü ve bir
POST rotası ekler. **Migration yok — ölçüldü:** `employees.email` 00003'ten beri `citext` ve
tenant içi kısmi tekil indeksli (B14); `tappa_app` tabloya UPDATE (00003) ve
`employee_invites.cancelled_at`'e sütun UPDATE'i (00012) taşır; `FOR UPDATE` satır kilidi UPDATE
yetkisiyle alınır. `go.mod`/`go.sum`/`sqlc.yaml` diff boş. DB testleri dev Postgres'e test
tenant'ı, çalışan, davet ve audit satırı yazar (öteki DB testleri gibi); süper kullanıcı sondası
tek bir transaction'da koşar ve geri alınır.

**Yazıldı:** `db/queries/employees.sql` — `GetEmployeeEmail`, `LockEmployeeForEmailChange`
(`FOR UPDATE`), `SetEmployeeEmail` · `internal/domain/tenant/staffemail.go` — `Staff.Email`,
`Staff.ChangeEmail`, `ErrSameEmail`, `ActionEmployeeEmailChanged` · `internal/mail` —
`ValidRecipient` (yeni dışa açık işlev; gövdesi `validRecipient`'in kendisidir; `doc.go`'nun
PART I'inde bir madde) ·
`internal/handler/employeeemail.go` — `POST /admin/employees/email` (`mountWriting`, yani
`ProtectWriting`), `mayChangeEmployeeEmail`, `ActionEmployeeEmailChangeRefused` · eylem kartında
*"Email on file"* bölümü (`web/templates/components/roster.templ`) ve dört ret + bir bildirim
cümlesi (`web/templates/pages/employees.templ`).

**Kapsam kararı.** *Hangi adres:* `employees.email` — davetin gideceği adres. Yönetici adresi
(`admin_users.email`) bir giriş kimliği ve sıfırlama hedefidir, başka bir güvenlik sınırıdır
(ADR 0015); bu görevin değil. *Hangi ekran:* paneldeki eylem kartı (`?manage=<id>`); liste satırı
adres göstermez. Ekleme formu (M6-13) değişmedi.

**Kararlar (gerekçeli):**
1. **Yalnız owner değiştirir; her yönetici okur.** Öteki çalışan eylemleri (ekle, davet et,
   deaktive et, taşı) her yöneticiye açıktır; bu açık değildir, çünkü `email` modunda (EM-7) adres,
   çalışanın kimliğiyle telefon bağlayan bir kimlik bilgisinin GİTTİĞİ yerdir. Var olan bir
   çalışanın adresini kendi kutusuna çeviren bir manager onun bir sonraki linkini alırdı — Y-D'nin
   e-posta biçimi. K3 aynı modda linki bir yöneticinin önüne koymanın tek öteki yolunu (owner-only
   "linki göster" yedeği) zaten owner'a ayırır; bu ona uyar. Mekanizma `mayRemove`/
   `mayEditAccount` biçimidir (rol oturumdan, ek sorgu yok); reddedilen deneme 303
   `problem=not-permitted` ve `employee.email_change_refused` satırı (detail tam olarak `outcome`,
   `reason`, `role`, `required_role`; gönderilen değer YOK). **Elenen:** (a) her yönetici —
   yukarıdaki devralma; (b) policy motoru — `accountactions.go`'nun üç ölçümü aynen geçerli.
   **Sayılı:** B28 yüzünden kapı bugün hiçbir gerçek isteği ayırmaz (testler manager oturumunu
   fikstürle kurar); manager hâlâ istediği adresle birini EKLEYEBİLİR — sayılı sınır 2'nin
   "tek kutu N hayali çalışan" artığı, var olan birinin devralınması değil.
2. **Kural Send'in kuralıdır:** `mail.ValidRecipient` (§4), ikinci kopya yok —
   `TestValidRecipient_AgreesWithSend` onu Send'in `invalid_address` cevabına bağlar. Form
   sınırında yalnız çevreleyen boşluk kırpılır (`strings.TrimSpace`, sıfırlama formu emsali —
   kenardaki NBSP, U+2028, NEL ve CR LF de kırpılır, U+200B kırpılmaz ve kuralda reddedilir;
   go1.27.1 sondası); büyük/küçük harf korunur (tekillik `citext`'in); Unicode normalleştirmesi
   YOK — ASCII dışı her bayt reddedilir, yani ASCII DIŞI bir homoglyph (Kiril, tam genişlikli
   harf) de reddedilir. **ASCII içindeki benzerler geçer** (`rn`/`m`, `l`/`I`/`1`, `0`/`O`):
   onlar sıradan karakterlerdir ve kural onları ayıramaz (2. tur düzeltmesi — ilk metin
   *"homoglyph de reddedilir"* diyordu, geniş). Boş = adres kaldırılır (NULL; `''` kısmi tekil
   indekse girerdi).
3. **Beş adım, tek transaction, sıra = eşzamanlılık tasarımı** (`Staff.ChangeEmail`'in yorumu):
   bekleyen davetleri iptal (davet satırı kilitleri ÖNCE — aktivasyon ifadesinin sırası) → çalışanı
   `FOR UPDATE` kilitle (eşzamanlı `CreateInvite`'ın FK kontrolünün `FOR KEY SHARE`'iyle çakışan
   tek mod; düz UPDATE'in `FOR NO KEY UPDATE`'i çakışmaz) → TEKRAR iptal (beklerken commit edilen
   daveti yakalar) → adresi yaz (kilitlenen değere karşı karşılaştırarak) → iz (`RecordTx`). Her
   ret adım 1'den sonra transaction'ı geri alır — adım 1'in iptalleri dahil.
4. **Aynı adres = byte eşitliği** → `ErrSameEmail`, hiçbir şey yazılmaz/iptal edilmez/izlenmez.
   Yalnız harf büyüklüğü değişen adres BİR yazımdır ve linkleri iptal eder (güvenli yön).
5. **İz:** `employee.email_changed`, detail tam olarak `had_email`, `has_email`,
   `retired_invitations`; adres yok (§7: satır değişikliğin olduğunu gösterir, değeri göstermez).
   **Log:** yalnız `employee_id`, `actor_id`, `has_email`, `retired_invitations`.
6. **Sonuçların gittiği yer — ve hiçbirinde yazılan adres geri verilmez** (URL'ye konamaz:
   adres çubuğu, geçmiş, Referer). Başarı ve `bad-email`, `email-taken`, `same-email`,
   `not-permitted` retleri → kişinin kartına 303; `ErrUnknownEmployee` → kartsız listeye 303
   (`problem=unknown`); okunamayan gövde ya da uuid olmayan id → kartsız listeye 303
   (`problem=unreadable`); başka her hata → 500, **yazanın** sorun sayfası
   (`problemPanelWriteFailed`, *"We could not save that"* — 3. tur; önceden okuyanın
   *"We could not read your records"* sayfasıydı). (2. tur düzeltmesi — ilk metin
   *"her sonuç karta 303"* diyordu, koddan genişti.) Bildirim `"moved"` sınıfındadır:
   `done=email` aynı istekte okunan adresi basar, olayı değil.
7. **Kart okuması ayrı bir sorgudur** (`GetEmployeeEmail`) — `tenant.Person` adres alanı
   taşımaz ve taşımamaya devam eder; okuma başarısızsa kart hiç çizilmez (*"No address on file"*
   yalan olurdu).

**Sapma (§7'den, açıkça).** §7 adres okumasını *"yeni bir sqlc sorgusu … gövde için işletme
adıyla (EM-6)"* diye tarif eder ve onu `IssueAndDeliver`'dan ÖNCE koyar. `GetEmployeeEmail`
işletme adını seçmez: okuyucusu (kart) adı kullanmaz ve okuyucusuz sütun seçilmez. Daha önemlisi,
aşağıdaki sayılı sınır 1 EM-7'nin okumasının **yerini** değiştirir — davet basan transaction'ın
İÇİNDE, `CreateInvite`'tan SONRA — ve o sorgunun biçimi (işletme adı dahil) EM-7'nindir.

**Güvenlik iddiaları — üç parçalı (EM-6).** Tehdit modeli: *"Bu pinler bu dosyalara,
çağıranlarına ve üç sorguya kazara giren sapmaya karşıdır; bir pini atlatmak için bilerek
yazılmış kod, kod incelemesinin konusudur."* Mutasyonlar kopyala-değiştir-geri yaz yöntemiyle,
her birinin uygulandığı `diff` ile ve geri alındığı `cmp` ile doğrulanarak koşuldu; numaralar M10
EM-6 kartının tablosudur.

*İddia G (EM-6 eki) — A tenant'ı B'nin bir çalışanının adresini okuyamaz ve değiştiremez.*
- **PART I:** `TestRLS_EmployeeEmail_AnotherTenantsAddressIsNeitherReadNorWritten` — `tappa_app`,
  A'nın bağlamında, üç sorgu B'nin KENDİ tenant id'si ve çalışanıyla (yüklem eşleşirdi, yalnız RLS
  cevap verebilir) → satır yok; tenant yüklemsiz `WHERE id = …` sondası → 0; kontrol: B'nin
  bağlamında aynı okuma B'nin adresini verir.
  `TestEmployeeEmailQueries_CarryTheirOwnTenantPredicate` — `tappa_owner` (süper kullanıcı, RLS
  uygulanmaz), A'nın tenant'ı + B'nin çalışanı → üç sorgu satır yok; kontrol: B'nin tenant'ıyla
  B'nin adresi; transaction geri alınır.
  `TestStaffEmailDB_ATenantCannotReadOrChangeAnotherTenantsAddress` — alan yolu: A'dan okuma ve
  değişiklik `ErrUnknownEmployee`; B'nin adresi, linki ve iki tenant'ın izi değişmez; kontrol: A
  aynı adresi kendi çalışanına yazabilir (tekillik işletme başınadır).
- **PART II:** sorgunun kendi tenant yüklemini silen M17/M18/M19 →
  `TestEmployeeEmailQueries_CarryTheirOwnTenantPredicate` ve
  `TestStaffQueries_CarryAnExplicitTenantPredicate` kırmızı (RLS testi bu üçünde yeşil kalır —
  ölçüldü; o açık yüklemi değil RLS'i ölçer). **Ölçülmeyen:** `employees` RLS politikasını gevşeten mutasyon bu
  görevde koşulmadı (paylaşılan dev Postgres'te DDL yok); RLS testinin onu yakalayacağı bir tasarım
  iddiasıdır, ölçüm değil.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia K — bir değişiklik, kişinin harcanabilir davetlerini aynı transaction'da iptal eder ve
tek bir iz satırı yazar; reddedilen değişiklik hiçbir şeyi iptal etmez.*
- **PART I:** `TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction` (reddeden izle adres ve
  iki link aynı kalır; gerçek izle iki bekleyen iptal, süresi dolmuş/harcanmış/meslektaşınki
  dokunulmaz, tek satır, anahtarlar tam üç, adres yok) ·
  `TestStaffEmailDB_ARefusedChangeRetiresNothing` (aynı adres, kırpılmış aynı adres, başka
  büyüklükte meslektaş adresi, kuralı geçmeyen adres) ·
  `TestStaffEmailDB_AnInvitationBeingIssuedIsRetiredByTheChange` (açık tutulan bir basım
  transaction'ı varken değişiklik 400 ms içinde bitmez; basım commit edince yeni davet iptal) ·
  `TestStaffEmailDB_AnActivationInFlightDoesNotDeadlockTheChange` (davet kilidini tutan aktivasyon
  taklidi ve değişiklik ikisi de başarılı, 40P01 yok) ·
  `TestStaffEmailDB_TwoOwnersSavingTheSameAddressWriteOnce` (25 tur × 8 yarışçı, her tur tam bir
  yazım) · `TestStaffEmailDB_ACapitalsOnlyChangeIsAWrite` (2. tur: yalnız harfi değişen adres
  bir yazımdır — yeni yazım saklanır, link iptal, tek iz satırı) · `TestEmployeeEmailDB_AChangedAddressKillsTheLinkSentBefore` (gerçek HTTP: değişiklikten
  önce basılan link aktive etmez; kontrol: sonra basılan eder).
- **PART II:** M01 (adım 1 yok → 40P01), M02 (adım 3 yok → yeni davet yaşar), M03 (`FOR UPDATE`
  yok → iki test), M04 (`FOR NO KEY UPDATE` → değişiklik beklemez), M05 (karşılaştırma yok), M07
  (iz yazılmaz), M08 (iz hatası yutulur), M09 (detail'de adres), M40 (iptal süresi dolmuşu da
  kapsar) — hepsi kırmızı. **Yakalamadığı:** sayılı sınırlar 1–4.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia L — saklanan adres, Send'in kabul edeceği adrestir.*
- **PART I:** `TestValidRecipient_AgreesWithSend` (39 değer — `TestSend_RefusesBadInputBeforeAnyDial`'ın
  `To` satırları ve kontrolleri ile Kiril/tam genişlikli harf, sıfır genişlikli boşluk, NBSP, RLO,
  çift `@`, boş taraflar: ikisi de 6 kabul, 33 red; red hiç bağlanmaz) ·
  `TestStaffEmailDB_TheRuleIsTheSendRule` (reddedilen hiçbir şey saklanmaz; kabul edilen kırpılıp
  bayt bayt saklanır, harf korunur; 254 bayt kabul, 255 red; boş = NULL, ikinci adressiz kişi
  yasal).
- **PART II:** M10 (zayıf kural), M11 (kırpma yok), M12 (boş → `''`), M20 (küçük harfe çevirme),
  M35 (ASCII'siz kopya kural), M36 (sabit doğru) — kırmızı.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia M — yalnız owner değiştirir; manager'ın denemesi işletmenin izine düşer; adres hiçbir log
satırına, yönlendirmeye ya da iz satırına girmez.*
- **PART I:** `TestEmployeeEmail_OnlyAnOwnerReachesTheDomain` · `TestEmployeeEmail_TheCardOffersTheFormOnlyToAnOwner` ·
  `TestEmployeeEmailDB_AManagersAttemptLandsInTheBusinessesTrail` ·
  `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress` ·
  `TestEmployeeEmail_IsBehindTheWriteChain` (çapraz köken 0 çözücü okuması, anonim → giriş,
  büyük gövde → `unreadable`) · `TestEmployeeEmail_AFailedAddressReadShowsNoCard` ·
  `TestEmployeeEmail_AStoredAddressIsEscapedWhereItIsRendered` (ekleme formunun zayıf kuralıyla
  saklanabilen işaretli bir adres metinde ve `value`'da kaçışlı) ·
  `TestStaffEmailDB_TheDomainNeverLogsTheAddress` · `scripts/redline-check.sh` R7b.
- **PART II:** M16 (alan log'unda adres — test ve R7b kırmızı), M21 (kapı her role açık), M22
  (ret izi yok), M23 (ret izinde adres), M24 (sunucu kapısı yok), M25 (tenant gövdeden), M26
  (başarı log'unda büyük harfli adres), M27 (adres yönlendirmede), M28 (aynı adres başarı diye),
  M29 (form herkese), M30 (okuma hatası yutulur), M31 (manager cümlesi yok), M32 (adres satırı
  boş — yalnız manager kolunda), M33 (rota takılı değil), M34 (rota okuma zincirinde), M37
  (bildirim olay iddia eder), M43 (bilinmeyen kişi 500), M44 (adres `templ.Raw` ile) — kırmızı.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

*İddia N (3. tur) — yarıda düşen bir değişiklik hiçbir şey yazmaz ve yöneticiye bunu yazanın
sayfasıyla söyler; tekrar basmak iki kez yazmaz.*
- **PART I:** `TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce` — gerçek HTTP,
  gerçek Postgres, gerçek `Staff`; yalnız izi reddeden, teste özel `peekingTrail` ile (4. tur;
  paylaşılan `failingTrail`'e dokunulmadı). Reddetmeden ÖNCE, kendisine verilen `tx` üzerinden —
  değişikliğin kendi, commit edilmemiş transaction'ı — o anki durumu okur ve test bunu ister:
  **adres yeni değerde, harcanabilir link 0** (ölçüldü: ikisi de öyle). Yani iz gerçekten son
  adımdır ve adres yazımı ile iptal, iz düştüğünde transaction'ın İÇİNDEDİR; aşağıdaki sıfırlar
  bunların geri alındığını gösterir, hiç başlamamış bir değişikliği değil. (3. turda bu yalnız
  çağrı sayısından ve kodun sırasından çıkarılıyordu — denetçinin E6b'si yeşil kaldı, aşağıda
  A41.) Cevap 500, gövde *"We could not save that"* ve *"nothing was written"*; reddeden iz tam 1
  kez çağrıldı. **Sayılar:** adres eski değerinde, bayt bayt · harcanabilir link 1 → 1
  (iptal edilmedi) · `employee.email_changed` 1 → 1 · kişi hakkında HERHANGİ bir eylemdeki iz
  satırı 2 → 2 (handler'ın izi gerçek olduğundan onun yazdığı bir satır da sayılırdı) · log satırı
  adres taşımaz. Sağlıklı panelde tekrar: 303 `done=email`, adres yeni, link 0, eski link aktive
  etmez, `employee.email_changed` 1 → 2 (tek yazım). Aynı adres bir kez daha (arada yeni bir link
  basılmış): 303 `problem=same-email`, `employee.email_changed` 2 → 2, iz satırları 5 → 5, yeni
  link 1 → 1 (iptal EDİLMEDİ — ret, adım 1'in iptalini de geri alır), adres değişmedi ·
  `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress` (500 satırı yeni gövdeyi
  okur; 4. turdan beri `tenant.wrap` gibi sarılmış bir `*pgconn.PgError{Code: "40P01"}` ayrı bir
  satırdır ve aynı sayfayı ister) · `TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty`
  (`employeeEmail` listede değil; önce `mountWriting`'in onu kaydettiğini ister, yoksa iddia boş
  küme üzerinde tutardı) · alan katmanında
  `TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction`'ın reddeden iz kolu.
- **PART II:** A39 (500 okuyanın sayfasına geri döner → sayım testi, sonuç tablosunun 500 satırı
  ve DB testi kırmızı) · A40 (iz satırı, değişiklik commit edildikten SONRA ayrı bir
  transaction'da → handler'ın ve alanın atomiklik testleri kırmızı; ölçülen hâli: adres yeni,
  link 0, iz satırı yok ve tekrar `same-email` ile reddedilir — yani iz kalıcı olarak kayıp).
  2. turun A23'ü de bu testi kırmızıya çevirir (küçük harfe çevrilen eski adres bayt bayt
  eşleşmez). · A41 (= denetçinin E6b'si: iz yazımı aynı transaction'ın BAŞINA taşındı) → handler
  testi kırmızı (`tx` içi okuma: adres eski, link 1; 500 sonrası sayılar sağlıklı koşuyla AYNI —
  yani yalnız sayılar bu mutasyonu göremez) ve alanın testi kırmızı · A42 (= E4: `*pgconn.PgError`
  alt kümesi yeni bir yardımcı metot üzerinden okuyanın sayfasına) → sonuç tablosunun 40P01
  satırı kırmızı (sayım testi bu mutasyonda yeşil kalır: yalnız handler'ın kendi gövdesini okur).
  **Yakalamadığı:** sayım, handler'ın çağırdığı yardımcıların içini görmez; o alt kümenin pini
  40P01 satırıdır, başka bir `PgError` kodu için ayrı satır yok.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**EM-6'nın sayılı sınırları (bu ADR'nin sınırlarına ek):**
1. **EM-7'nin okuma yeri — açık yarış, ölçülmedi (EM-7 kodu yok).** Değişiklik commit'ten SONRA
   basılan davet canlıdır (değişiklikten sonra basıldı) ve hangi adrese gideceği EM-7'nin
   okumasına bağlıdır. §7'nin bugünkü cümlesi (*"`IssueAndDeliver`'dan önce"*, ayrı transaction)
   bu sırayı açık bırakır: eski adres okunur → değişiklik commit → davet basılır (adım 3 onu
   göremez) → eski adrese canlı bir kod. Kapanan biçim: adres, davet basan transaction'ın İÇİNDE,
   `CreateInvite`'tan SONRA okunur — `CreateInvite`'ın FK kilidi (`FOR KEY SHARE`) adım 2'nin
   `FOR UPDATE`'iyle çakışır, yani okuma ya değişikliğin yeni adresini görür ya da değişiklik onu
   bekler ve adım 3'te o daveti iptal eder. Mekanizmanın kendisi
   `TestStaffEmailDB_AnInvitationBeingIssuedIsRetiredByTheChange` ile ölçüldü.
2. **Deadlock penceresi:** değişikliğin birkaç ifadelik transaction'ı içinde hem basılıp hem
   HARCANAN bir davet adım 3 ile kilitlenir; biri 40P01 ile düşer, yarım iş kalmaz. Davetin
   "basılan" yarısı bu pencereye ancak adım 1 ile adım 2 ARASINDA commit edilirse girer (adım 1
   onu görmemiş, adım 3 görür). **Ölçüldü (EM-6 1. tur üçüncü göz; bu görevde yeniden
   koşturulmadı):** üç denemenin üçünde düşen (kurban) aktivasyon oldu, yönetici başarı gördü.
   Değişikliğin düştüğü yön ölçülmedi; düşerse yöneticiye yazanın sorun sayfası (*"We could not
   save that"*, 500 — 3. tur) gider — 40P01'e ayrı cümle yok. Sayfanın *"nothing was written"*
   cümlesi o yönde de doğrudur (Postgres transaction'ı bütün olarak iptal eder, İddia N).
   **Handler katmanında ölçülüyor (4. tur):** sarılmış bir `*pgconn.PgError{Code: "40P01"}`
   `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress`'te ayrı bir satırdır ve
   yazanın sayfasını, 500'ü ister (A42 kırmızı). **Ölçülmeyen:** veritabanında gerçek bir
   deadlock'ta değişikliğin düşen taraf olduğu koşu — yani 40P01'in gerçekten bu dala bu biçimde
   ulaştığı uçtan uca gösterilmedi.
3. **`FOR UPDATE` aynı kişinin FK ekleyen öteki yazımlarını** (tap kaydı, yeni oturum, davet)
   değişikliğin transaction'ı boyunca bekletir. **Ölçüldü (EM-6 1. tur üçüncü göz; bu görevde
   yeniden koşturulmadı):** değişiklikler koşarken aynı kişiye 100 tap — 100'ü kaydedildi; tap
   gecikmesi p50 93 → 135 ms, p90 201 → 274 ms, en çok 452 → 714 ms.
4. **Eşzamanlı FARKLI iki adres** ikisi de yazar, son yazan kalır, iz iki satır (aynı adres = tek
   yazım, pinli).
5. **Ekleme formunun kuralı zayıf kaldı** (M6-13, B14): ASCII dışı bir adresle eklenen kişinin
   adresi kartta olduğu gibi görünür, değiştirme formu aynı değeri kaydetmeyi `bad-email` ile
   reddeder; EM-7 basımda reddeder. Ekleme kuralını sıkılaştırmak sevk edilmiş bir akışı değiştirir
   — bu görevin değil.
6. **Yönetici adresine eşit çalışan adresi burada reddedilmez** — §7 o kapıyı basım anına koyar
   (EM-7).
7. **Owner kartındaki adres METİN satırı tek başına pinsiz** (M32 yalnız manager kolunda
   kırmızı; owner kolunda formun `value`'su aynı adresi taşır). Formun `value`'su ise 2. turdan
   beri ayrıca pinli: girdi id'siyle bulunur ve değeri kayıtlı adresle bayt-aynı olmalı (A33
   kırmızı).
8. **`wrap()`'taki `ErrSameEmail` satırı davranış-eşdeğerdir** (M14 yeşil): varsayılan dal `%w` ile
   sarar ve `errors.Is` görür; liste yalnız metni öneksiz tutar. M6-13'ün aynı listede aksini
   söyleyen cümlesi ölçümle düzeltildi.
9. **R7b'nin ara değişken sınırı** (`redline-check.sh` sınır 1): adresi bir yerel değişkene alıp
   loglayan satır taramadan geçer; testler yalnız ölçülen satırları kapsar.
10. **Tarayıcının `type="email"` alanı** IDN alan adlı bir adresi punycode'a çevirip gönderebilir;
    sunucu sonucu ASCII kuralıyla değerlendirir — ölçülmedi.
11. **Belirsiz commit doğrudan ölçülmedi** (3. tur). `COMMIT` sunucuya ulaşıp cevabı uygulamaya
    ulaşmazsa değişiklik yazılmış ama yönetici 500 görmüş olur; o zaman sayfanın *"nothing was
    written"* cümlesi yanlıştır. Ölçülen, o durumun SONRAKİ hâlidir: adres zaten kayıtlıyken aynı
    adresi tekrar basmak `same-email` ile reddedilir ve hiçbir şey yazmaz — yani *"pressing again
    will not enter it twice"* o durumda da tutar. Bu sınır bütün panel yazımlarınındır (manuel
    kayıt dahil), bu rotaya özgü değil. Yönetici araya FARKLI bir adres yazarsa bu ikinci bir
    değişikliktir (iki iz satırı) — "iki kez" değil.

**EM-6 2. tur (2026-10-06 — üçüncü göz RED, tek bloklayan bir test fikstürüydü; yalnız test ve
metin değişti, ürün kodunda yalnız yorum satırları):**
- **B1 (bloklayan):** handler testinin fikstür adresi tamamen küçük harfti, yani
  `TestEmployeeEmail_OnlyAnOwnerReachesTheDomain`'in *"ham değer iletilir"* iddiası küçük harfe
  çeviren bir handler'ı (A23) göremiyordu → fikstür büyük harfli
  (`Maria.ZZ7Q.Borg@Kebab.example.test`), sızıntı denetimleri küçük harfe çevrilmiş metinde
  arar; uçtan uca DB testinin adresi de büyük harfli → A23 KIRMIZI.
- **A11:** bayt eşitliği kuralını ölçen test yoktu (`EqualFold` mutasyonu yeşil) →
  `TestStaffEmailDB_ACapitalsOnlyChangeIsAWrite` → KIRMIZI.
- **A33:** formun `value`'su silinince test yeşildi (adres metin olarak da görünür) → owner
  kolunda girdi id'siyle bulunur, değeri kayıtlı adresle bayt-aynı → KIRMIZI.
- **A18:** üretilmiş `getEmployeeEmail`'in `lower(email)` döndürmesi yeşildi (bütün fikstürler
  küçük harf) → domain ve `internal/db` fikstürleri karışık harfli; kart okumasının bayt
  sadakati `TestStaffEmailDB_TheRuleIsTheSendRule` ve iki tenant testinin kontrollerinde → KIRMIZI.
- **A38:** ret yoluna bir adres okuması eklemek yeşildi (sahte `Email()` okumayı saymıyordu) →
  sahte üç çağrıyı da sayar; manager kolu yönlendirme izlenmeden ÖNCE üçünün de 0 olduğunu ister
  → KIRMIZI.
- **Metin:** karar 2 (ASCII içindeki benzerler geçer), karar 6 (sonuçların gerçek kümesi),
  sınır 2 ve 3 (denetçinin ölçümleri), sınır 7 (`value` artık pinli); `employeeemail.go`'da iz
  satırlarının içeriği ve sonuç kümesi; `compose.go`'da `ValidRecipient`'in ölçüldüğü küme
  (`doc.go` ile aynı cümle); `staffemail.go`'da homoglyph cümlesi.
  `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress` artık handler'ın her dalını
  sürer (okunamayan gövde, uuid olmayan id, manager, beş alan sonucu, 500 — 500'ün gövdesi de
  okunur).

**EM-6 3. tur (2026-10-06 — koordinatör kararı: 2. turun bloklamayan gözlemi, 500'ün okuyanın
sayfası olması, `problemPanelWriteFailed`'e geçer):**
- **Karar ve kapsam.** `employeeemail.go`'nun 500 dalı artık yazanın sayfasını gösterir
  (*"We could not save that … nothing was written … pressing again will not enter it twice"*);
  okuyanın sayfası (*"this page is not showing anything"*) yöneticinin az önce yaptığı şeyi
  anlatmıyordu. Ürün kodunda değişen **yalnız bu çağrı yeridir** (yorumsuz AST karşılaştırması:
  çağrı yeri geri çevrilince 2. turla aynı); `adminlogin.go`'da yalnız yorum. Öteki çalışan
  eylemleri (`employeeAdd`, `employeeInvite`, `employeeDeactivate`, `employeeMove`) başka
  ekranlarındır ve dokunulmadı.
- **Sayfanın iki iddiası bu rota için ölçüldü, ödünç alınmadı** — İddia N. Atomiklik: beş adımın
  hepsi tek `WithTenant` içindedir; son adım (iz) düşünce adres, link iptali ve iz birlikte geri
  alınır (sayılar İddia N'de). Durmayı gerektiren bir bulgu yok: değişiklik tek transaction'dır.
- **İkinci gönderim — ölçüldü, raporlandı:** aynı adresin ikinci kez gönderilmesi bir no-op'tur
  (`ErrSameEmail`, 303 `problem=same-email`); ikinci bir iz satırı YAZILMAZ (`employee.email_changed`
  2 → 2, kişi hakkındaki bütün iz satırları 5 → 5) ve arada basılmış link iptal edilmez. Ret,
  kilitli değerle bayt eşitliğine dayanır; yalnız harfi farklı adres bir yazımdır (karar 4).
- **Sayım:** `TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty`'nin tek
  iddiası artık `employeeEmail`'i de tutar (tam ad, önek değil) ve önce rotanın `mountWriting`'de
  kayıtlı olduğunu ister. Ölçülen sayım: okuyanın sayfası 41 kullanım, bunların 15'i bir yazma
  handler'ında (16 → 15); yazanın sayfası 5 kullanım.
- **Mutasyonlar** (`scratchpad/em6/verify_round3.py`, em6-orch kopyasında; taban YEŞİL, dosyalar
  sha ile geri yüklendi): 2. turun beşi (A23, A11, A33, A18, A38) yine KIRMIZI; A39 (500 okuyanın
  sayfasına geri) → sayım testi, `EveryOutcome…/database` ve yeni DB testi KIRMIZI; A40 (iz ayrı
  transaction'da, değişiklikten sonra) → `TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce`
  ve `TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction` KIRMIZI.
- **Metin:** karar 6 ve sınır 2 yeni sayfayı adlandırır; sınır 11 (belirsiz commit) eklendi.

**EM-6 4. tur (2026-10-07 — 3. turun dar kapanış denetimi ONAY, bloklayan yok; dört DÜŞÜK bulgu
commit'ten önce kapatıldı. Yalnız test ve metin: ürün kodunda davranış değişmedi, yalnız
`employeeemail.go`'da yorum):**
- **Yorum düzeltmesi.** `employeeemail.go`'nun 500 dalındaki yorum aynı adresin *"before any
  write"* reddedildiğini söylüyordu; adım 1'in iptali (bir UPDATE) karşılaştırmadan ÖNCE koşar ve
  ret onu geri alarak gelir. Yorum artık *"refuses as ErrSameEmail and rolls back, step 1's
  retirement included"* der (`staffemail.go`'nun kendi yorumu, İddia N ve kart bunu zaten doğru
  yazıyordu).
- **Yeni kanıt 1 — "önceki dört ifade koştu" artık ölçülüyor.**
  `TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce`'ın kırık izi teste özel
  `peekingTrail`'dir: reddetmeden önce kendisine verilen `tx` üzerinden adresi ve harcanabilir
  linkleri okur; test **adres yeni, link 0** ister (ölçüldü: ikisi de öyle). Denetçinin E6b'si
  (A41) artık bu testte de KIRMIZI; A41 altında `tx` içi okuma *adres eski, link 1* verdi ve 500
  sonrası sayılar sağlıklı koşuyla birebir aynıydı — eksik olan tam olarak bu okumaydı.
- **Yeni kanıt 2 — 40P01 satırı.** `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress`'e
  `tenant.wrap` gibi sarılmış `&pgconn.PgError{Code: "40P01"}` döndüren `deadlock` satırı eklendi:
  500, yazanın sayfası. Denetçinin E4'ü (A42: `PgError` alt kümesi bir yardımcı metot üzerinden
  okuyanın sayfasına) artık bu satırda KIRMIZI. Sınır 2 buna göre düzeltildi: 40P01 handler
  katmanında ölçülüyor, veritabanında gerçek bir deadlock ölçülmedi.
- **Mutasyonlar** (`scratchpad/em6/verify_round4.py`, em6-orch kopyasında; taban YEŞİL; 4 dosya sha
  ile geri yüklendi, kopya worktree ile `diff -rq` aynı): A23, A11, A33, A18, A38, A39, A40 yine
  KIRMIZI (A39 artık `EveryOutcome…/deadlock`'u da düşürür); **A41** KIRMIZI —
  `TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce` ve
  `TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction`; **A42** KIRMIZI —
  `TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress/deadlock`. ALL RED.
- **Metin:** İddia N'nin PART I ve PART II'si, sınır 2; kartın iddia özeti (G, K, L, M, N).

**EM-6 5. tur (2026-10-07 — güvenlik denetimi ONAY; tek DÜŞÜK bulgu commit'ten önce kapatıldı.
Yalnız test: ürün kodu ve yorumları aynen kaldı):**
- **Bulgu:** rol kapısının (`mayChangeEmployeeEmail`) rolü NEREDEN okuduğu ve hangi rolleri
  geçirdiği sabitlenmemişti. Denetçinin iki mutasyonu bütün EM-6 testlerinde yeşil kaldı: S10
  (kapı gövdedeki `role=owner`'ı da kabul eder) ve S2 (izin listesi yasak listesine döner:
  `Role != "manager"`). Tenant için bu sabitleme vardı (M25), rol için yoktu.
- **Yeni kanıt (İddia M'nin PART I'indeki `TestEmployeeEmail_OnlyAnOwnerReachesTheDomain`):**
  her kolun formu artık `role=owner` taşır (yabancı `tenant_id` ve `actor_id`'nin yanında).
  Reddedilen kollar üçtür: `manager`, canlı oturum + boş rol (`no role`) ve canlı oturum +
  tanımsız rol (`auditor`, `undefined role`). Her biri için beklenen: 303
  `problem=not-permitted`, staff yüzeyine 0 çağrı (Person okuması, adres okuması ve değişiklik
  0), tam bir `employee.email_change_refused` satırı ve satırın `role`'ü OTURUMUN rolü — gönderilen
  `owner` değil.
- **Mutasyonlar** (`scratchpad/em6/verify_round5.py`, em6-orch kopyasında; taban YEŞİL; 4 dosya sha
  ile geri yüklendi, kopya worktree ile `diff -rq` aynı): önceki dokuzu (A23, A11, A33, A18, A38,
  A39, A40, A41, A42) yine KIRMIZI; **A43** (= S10) KIRMIZI — `manager`, `no role` ve
  `undefined role` kollarının üçü; **A44** (= S2) KIRMIZI — `no role` ve `undefined role` kolları
  (`manager` kolu bu mutasyonda doğal olarak yeşil kalır). ALL RED.
- **Sınır:** kartın formu POST kapısıyla aynı yüklemi paylaşır (`employees.go`, `CanChangeEmail`);
  A44 ikisini birlikte değiştirir ve POST kolları onu yakalar. Ama
  `TestEmployeeEmail_TheCardOffersTheFormOnlyToAnOwner` yalnız owner ve manager'ı sürer: yüklemi
  değil yalnız formun gösterimini tanımsız bir role açan bir değişiklik bu testlerde görülmez.
  Form gösterimi tek başına yetki vermez — POST kapısı ayrıca sabitli.

**Devirler — EM-7:** adres okumasını davet basan transaction'a, `CreateInvite`'tan sonraya koy
(sınır 1; §7'nin *"`IssueAndDeliver`'dan önce"* cümlesi buna göre güncellenir) · işletme adı o
sorguda · yönetici adresine `citext` eşitlik kapısı basımda · ASCII dışı saklanmış adres → davet
reddi · 40P01 için ayrı bir cümle istenirse · *Karar verilmedi*'deki anahtarlı alıcı özeti bu
satırı (`employee.email_changed`) da kapsar.

## EM-9 notu — 2026-10-06 (uygulama; normatif içerik: §8'in EM-9 cümlesi — kararlar, sapmalar ve ölçümler aşağıda)

K12'nin (*"parolanız değişti" bildirimi*) uygulaması. Taban dal ucu `4e69f84`. **ConfigMap
değişmedi** (`none` / `panel`): bugün canlıda e-posta **gönderilmez**, ama her parola
değişikliği artık bir bildirim satırı yazar (*"gönderilmedi, bu dağıtım e-posta göndermiyor"*)
— davranış değişikliği yalnız `audit_log`'dadır. Migration yok, `go.mod`/`go.sum`/`sqlc.yaml`
diff boş; yeni sorgu yok (adres var olan `GetAdminByID` ile okunur).

**Yazıldı:**
- `web/templates/email`: `RenderPasswordChanged`, `PasswordChangedView` (yalnız `BaseURL`),
  `checkSignInLink` (taban `validBase`'den geçer, link `taban + "/admin/login"`'e **eşit**
  olmak zorunda), `signInPath`, sabit ASCII konu `Your Taptime password was changed`,
  `passwordChangedLetter` (argümansız — ad yok, an yok, adres yok); markup değişmedi
  (`message.templ` dokunulmadı).
- `internal/adminauth`: `Resets.NoticeRecipient` (var olan `recipient`'in dışa açık hâli:
  satırın kendi adresi, kendi tenant'ında; pasif/adressiz/başka tenant → `""`).
- `internal/handler`: `passwordnotice.go` (yeni — `passwordChanged`, `notice`, `recordNotice`,
  `containNotice`, `PasswordNotice`, sabitler, iki audit eylemi, altı sabit neden, hesap başına
  tavan); `ResetChannel`'a `DeliverPasswordNotice`; `emailResetChannel.DeliverPasswordNotice` +
  `signInLink` + kurucuda bildirimin de açılışta bir kez render edilmesi; `panelResets`'e
  `NoticeRecipient`; `AdminReset`'e `noticeLimiter`, `noticeSendGrace`; `Submit`'in başarı
  dalında ve `accountPasswordSave`'in başarı dalında `passwordChanged` çağrısı;
  `NewAdminAuth`'a **zorunlu** `notices` parametresi (nil ve tipli nil reddedilir).
- `cmd/tappa/main.go`: kurtarma akışı panelden **önce** kurulur ve panele bildirici olarak
  verilir.
- Metin eşitlemesi: `internal/config/config.go` (`ResetDelivery` yorumu),
  `deploy/k8s/05-config.yaml` (yalnız yorum; değerler aynı), `deploy/README.md` (iki yer).
- Testler: `web/templates/email/email_test.go` (`renderBoth` → `renderEach`, üç ileti; iki yeni
  test), `internal/handler/passwordnotice_test.go` (yeni), `internal/adminauth/resetrequest_db_test.go`
  (bir yeni test), `cmd/tappa/shutdownbudget_test.go` (bir yeni test),
  `cmd/tappa/passwordnotice_wiring_test.go` (yeni, 2. tur); `NewAdminAuth`'u çağıran mevcut **32**
  test çağrısı (**24** dosya) yeni argümanı aldı — **31**'i `&fakeNotices{}`, panel DB harness'ındaki
  (`adminlogin_db_test.go`) gerçek kurtarma akışını; yeni test dosyasının **2** çağrısıyla toplam
  **34** çağrı / **25** dosya (2. turda yeniden sayıldı; 1. tur metni *"32 çağrıya
  `&fakeNotices{}`"* diyordu); sekiz sahte kanala/çözücüye yeni metot (altısı
  `internal/handler`'da, ikisi `cmd/tappa`'da); iki DB testi **bilerek** genişletildi: `TestPanelRecoveryDB_EndToEnd` (bildirim
  satırın adresine, tek `sent` satırı, reddedilen tekrar bildirim yok) ve
  `TestPanelRecoveryDB_EndToEndThroughTheSMTPTransport` (rölede artık **iki** ileti: link ve
  bildirim).

**Kapsam (ölçüldü):** `admin_users.password_hash`'e yazan üretim ifadesi **iki**:
`SetOwnAdminPassword` (`db/queries/admins.sql`, çağıranı yalnız `Manager.ChangeOwnPassword`
← `accountPasswordSave`) ve `ConsumePasswordResetAndSetPassword` (`db/queries/passwordresets.sql`,
çağıranı yalnız `Resets.Consume` ← `AdminReset.Submit`); `CreateAdminUser` ilk paroladır,
değişiklik değil. Bildirim **ikisinde de** gönderilir. `op_complete_enrollment`'ın
`password_hash`'i `platform_admins`'tir (operatör yüzeyi e-posta göndermez — ADR 0020).

**Kararlar (gerekçeli):**
1. **Tek anahtar `TAPPA_RESET_DELIVERY`.** Bildirim de sıfırlama linki gibi yöneticinin kendi
   satırındaki adrese, aynı röleyle gider; yönetici e-postasını açmak **bir** deploy kararıdır.
   `none` → gönderim yok, adres okunmaz, `admin.password_notice.undelivered` satırı (neden:
   *"this deployment sends no e-mail…"*). Kartın *"ResetDelivery modu `panel`"* ifadesi
   ölçüldü: bu akışın kapalı kümesi `{none, email}`'dir (`internal/config`), `panel` davet
   akışının değeridir.
2. **İstekte, senkron — kuyrukta değil.** Kuyruk (§6) bir istekçinin **kayıtlı adresi
   numaralandırmasını** gizlemek için vardır; bildirimin gizleyecek bir sorusu yoktur (parolayı
   değiştiren hesabın var olduğunu bilir). Kuyruk ise tek bir FIFO'dur (32), herkese açık
   formdan doldurulabilir ve doluyken taşanı **göndermeden** `undelivered` yazar: kuyruğa giren
   bildirim, bir hesabı ele geçiren birinin anonim trafikle **susturabileceği** bir alarm
   olurdu. *(3. tur: bu gerekçe **kuyruk** için ölçüldü; aynı anonim trafiğin planlanan §9
   devre kesicisini açıp bildirimi susturabileceği ayrı bir yoldur — sınır 11.)* Ölçüldü: kuyruk
   doluyken bildirim kanala ulaşır
   (`TestPasswordNotice_AFullResetOutboxDoesNotStopIt`; kuyruğa dolu davranışını taklit eden
   mutasyon KIRMIZI). Bedel yanıt süresidir, ölçüldü: röle veri sonu yanıtını 2 s tuttuğunda
   hesap POST'u **2,002–2,007 s** (üçer koşu, `-race`'li ve `-race`'siz; sahte
   `ChangeOwnPassword` ile — gerçek değişikliğin bcrypt'i bunun üstüne), anında yanıt veren test
   rölesiyle 1,7–8,1 ms. Üst sınır `PasswordNoticeSendGrace` (10 s, adres okuması dahil) +
   `PasswordNoticeRecordGrace` (5 s) — HTTP boşaltmasının (20 s) içinde, değişikliğin kendisine
   5 s bırakarak (`TestShutdownBudget_ThePasswordNoticeNestsInsideTheHTTPGrace`).
3. **Hesap başına tavan: saatte 5 gönderim** (`passwordNoticeLimit`/`passwordNoticePeriod`,
   `httpx.Limiter.TryCharge` ile tek adım). Gerekçe: kayıt açık ve doğrulamasız, yani biri
   satırında **başkasının** adresi olan bir hesaba sahip olabilir ve her parola değişikliği o
   adrese bir ileti yollar; hesap bölümünün yazma zinciri oturum başına 10 dakikada 300 istek
   (`adminSessionLimit`) taşır — tavansız bir hesap bir posta bombasıdır. **Adres başına
   DEĞİL, hesap başına:** adres anahtarlı bir tavanı, kurbanın adresini taşıyan kendi hesabıyla
   önceden harcayan saldırgan, kurbanın gerçek hesabı ele geçirildiğinde **asıl** bildirimi
   susturabilirdi; hesap anahtarlı tavanı harcamak o hesabın parolasını değiştirmeyi gerektirir
   ve tavandan önceki her değişiklik bildirim yollar. Tavanın ötesinde değişiklik yine geçerli,
   satır yine yazılır (`undelivered`, tavanın nedeni); ilk aşımda bir log satırı.
4. **Ad yok.** Bir e-postada işletme ya da kişi adının gösterilip gösterilmeyeceği EM-7'nin
   bekleyen ürün kararıdır (sınır 22); bildirim ad **taşımaz** — görünümü yalnız `BaseURL`'dür
   ve iki render bayt bayt aynıdır.
5. **Link denetimi eşitlikle** (`checkSignInLink`): `checkLink`'in `?<param>=` biçimi uymaz;
   taban `validBase`'den, link `taban + "/admin/login"` ile birebir. Link **kanalda**,
   açılışta verilen `cfg.BaseURL`'den kurulur (`signInLink`); kanal isteği hiç görmez, yani
   `Host`/`X-Forwarded-Host`/`Forwarded` linke ulaşamaz (ölçüldü). `signInPath`,
   `adminLoginPath`'in ikinci kopyasıdır ve **tutulur**: kurucunun açılıştaki bildirim render'ı
   ikisi ayrışırsa açılışı durdurur.
6. **Audit:** `admin.password_notice.sent` / `admin.password_notice.undelivered`; değişiklik
   başına **tam bir** satır; detail `outcome`, `via` (`recovery`|`account`), `reason`, `class`,
   `smtp_code` — adres için alan **yok**. Bütçesiz: hacmi değişikliklerin kendi (bütçesiz)
   satırlarıyla bire bir. Satır değişikliğin satırından **sonra** yazılır.
7. **§4.6:** bildirim değişikliği geri almaz ve yanıtını değiştiremez; kanal ya da kayıt
   yazıcısı panik ederse panik **burada** yakalanır (router'ın Recoverer'ı commit'lenmiş bir
   değişikliği 500'e çevirirdi), değeri loglanmaz; satır yazımı başlamadıysa `undelivered`
   (neden: iç hata).
8. **`NewAdminAuth`'ın bildiricisi zorunlu** (M5-04): bildiricisiz bir panel, kimseye
   söylenmeyen ve bildirim satırı bırakmayan parola değişiklikleri yapardı.

**Sapmalar (açıkça):** EM-5A devrinin önerdiği kuyruk kullanılmadı (karar 2) · bildirim için
`TAPPA_RESET_DELIVERY`'den ayrı bir anahtar yok (karar 1) · §10'un anahtar kümesine yeni anahtar
eklenmedi (`tenant_id`, `admin_user_id`, `err_type`, `class`, `smtp_code`, `message_id` zaten
kümede).

**Güvenlik iddiaları — üç parçalı (EM-9).** Tehdit modeli: *"Bu pinler bu dosyalara, iki
çağırana ve e-posta kanalının bildirim metoduna kazara giren sapmaya karşıdır; bir pini atlatmak
için bilerek yazılmış kod, kod incelemesinin konusudur."*

*İddia K — her parola değişikliği tam bir bildirim satırıyla biter; bildirim, satırın kendi
adresine, tek URL'si yapılandırılmış giriş sayfası olarak ve hiçbir kimlik bilgisi taşımadan
gider; değişikliği ve yanıtını hiçbir koşulda değiştirmez.*
- **PART I:** `internal/handler/passwordnotice.go`'nun PART I'i, her madde test adıyla —
  başlıcaları: `TestPasswordNotice_EveryChangeEndsInOneRowAndTheChangeStands` (sekiz sonuç +
  kayıt yazıcısının paniği), `TestAccountPasswordSave_NotifiesAfterACommittedChangeOnly`,
  `TestAdminReset_ACompletedRecoveryNotifiesAndStillSignsIn`,
  `TestPasswordNotice_GoesToTheRowsAddressWithTheConfiguredSignInLinkOnly` (düşmanca `Host`,
  `X-Forwarded-Host`, `Forwarded` ile, gerçek taşıyıcı + test içi röle),
  `TestPasswordNotice_LogsOnlyIdsClassAndCode`, `TestPasswordNotice_AnAddressTheRelayCannotTakeIsNeverDialled`,
  `TestPasswordNotice_AFullResetOutboxDoesNotStopIt`,
  `TestPasswordNotice_ThePerAccountCapIsExactUnderConcurrentChanges` (40 eşzamanlı değişiklik ×
  200 koşu), `TestPasswordNotice_AHungRelayHoldsTheAnswerOnlyForTheSendGrace`,
  `TestPasswordNotice_AVisitorWhoLeavesStillGetsTheNotice`, `TestPasswordNotice_TheRowCarriesNoAddress`,
  `TestPasswordNotice_EachStepHasItsOwnBound` (2. tur: asılı adres okuması gönderim süresinde,
  asılı satır yazımı `PasswordNoticeRecordGrace`'te — sevk edilen 5 s — bırakılır),
  `TestPasswordNotice_TheShippedClocksAndCapAreWired` (2. tur: tavan ve iki süre literal değere ve
  bu notun cümlelerine bağlı), `TestNoticeRecipient_IsTheAddressOnTheRowAndNothingElse` (gerçek
  Postgres), `cmd/tappa`'nın kaynak pini `TestPasswordNoticeWiring_ThePanelIsGivenTheMountedRecoveryFlow`
  (2. tur: panelin bildiricisi, kanalı `NewEmailResetChannel`'dan doldurulan, bağlanan ve
  boşaltılan tek kurtarma akışıdır), şablon tarafında
  `TestRender_RefusesASignInLinkOutsideTheSignInPage` ve
  `TestPasswordChanged_IsTheSameWordsForEveryAccountAndCarriesNoCredential`.
- **PART II:** M10 EM-9 kartının mutasyon tablosu (1. tur 45 mutasyon: 44 KIRMIZI, 1 YEŞİL; 2. tur
  7 mutasyon, yedisi KIRMIZI — toplam 52: 51 KIRMIZI, 1 YEŞİL; hepsi kopyada).
  **Yakalamadığı:** kanal kurucusunun açılıştaki bildirim render'ının kaldırılması (YEŞİL —
  yalnız iki yol kopyası ayrıştığında önem taşır, o zaman röle testi gönderimde kırmızıdır);
  rölenin, süre konuşmayı kestikten sonra kabul ettiği gönderim (`undelivered` yazılır — sınır
  15'in biçimi); değişiklik ile satır arasında süreç ölümü (satır yok); parolayı değiştiren
  yeni bir çağıranın `passwordChanged`'i çağırmaması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**EM-9'un sayılı sınırları (bu ADR'nin sınırlarına ek; M10 EM-9 kartındaki listeyle aynı):**
1. **Gerçek röle (SES) ölçülmedi** — EM-5B; ConfigMap `email`'e çevrildiğinde bildirim de açılır,
   yani EM-5B'nin ölçümleri bu iletiyi de kapsamalıdır.
2. **Yanıt röleyi bekler:** hesap POST'u ve kurtarma `Submit`'i bildirim süresince
   (en çok `PasswordNoticeSendGrace`) bekler — ölçüldü, röle 2 s → yanıt 2,00 s.
3. **Alıcı başına tavan yok — sınır 13 bu iletiyi de kapsar, daraltılmış hâliyle.** Tavan
   **hesap başına** (saatte 5) ve **süreç başına**dır; sabit pencere (sınırda 2× patlama). Kurbanın
   adresini taşıyan **birden çok** hesap (kayıt açık, doğrulamasız: kaynak adres başına saatte 3
   tenant, ömür boyu tavan yok — B17) tavanı çarpar; dağıtık kaynakta sınır yoktur. EM-5B
   önkoşulu (b) bu iletiyi de kapsar.
4. **Parola değişikliğinin kendine özgü bütçesi yok:** hesap yolu yazma zincirinin bütçeleriyle
   (adres başına 10 dakikada 3000, oturum başına 300) ve her değişiklikte üç cost-12 bcrypt ile;
   kurtarma yolu `adminResetSubmitLimit` (adres başına 10 dakikada 10) ve link başına en çok bir
   değişiklikle sınırlıdır.
5. **Kayıt yazıcısının içindeki panik 0 ya da 1 satır bırakır** (EM-5A sınır 3'ün ikizi);
   ikinci satır denenmez — ölçüldü (yazıcıya tek çağrı).
6. **Yöneticinin adresini değiştiren bir ürün yolu bugün yok** (ölçüldü: `admin_users`'a UPDATE
   yazan üç sorgu `last_login_at`'e ve iki kez `password_hash`'e yazar, `email`'e değil). Biri
   gelirse, adres okuması ile gönderim arasında değişen adresin hangisine gideceği — ve
   değişikliğin ESKİ adrese de bildirilip bildirilmeyeceği — o görevin sorusudur.
7. **Hesap yolu gerçek Postgres'e karşı uçtan uca koşulmadı:** `ChangeOwnPassword` cost-12
   `Hash` öder (`-race` altında ~11 s); parçaları ayrı ölçüldü (adres okuması DB'de, handler
   sahte değişiklikle, kurtarma yolu DB'de uçtan uca).
8. **Açılış render'ı tek başına pinli değil** (mutasyon YEŞİL — PART II).
9. **§9 devre kesicisi yok** (EM-5A sınır 9; bildirim de ona sayılacak).
10. **Kablo pini kaynak düzeyindedir** (2. tur): `run()`'ın bildiriciyi ve kanalı **adlarıyla**
    bağladığını okur; `NewAdminReset`/`NewAdminAuth`'un argümanlarıyla ne yaptığını değil
    (`internal/handler`'ın testleri), ve kanal tanımlayıcısına `NewEmailResetChannel`'ın
    **yanında** başka bir değer daha atanmasını değil.
11. **Planlanan §9 devre kesicisi bildirimi susturabilir** (3. tur; güvenlik denetimi, ORTA —
    bugün kesici yok, yani bugün erişilebilir değil). §9 kesiciyi *"her gönderimi sayar"* diye
    tarif eder ve *Karar verilmedi*'de açıkken senkron `undelivered` önerilir. Kurtarma formu tek
    kaynak adresten saatte en çok ≈960 gönderim ister (B9, §9), kesici ≈300'de açılır: kesici
    böyle kurulursa saldırgan önce kesiciyi açar, sonra ele geçirdiği hesabın parolasını
    değiştirir ve bildirim — `undelivered` satırıyla, ama hesap sahibine ulaşmadan — susar.
    **Kural: kesici bildirimi sayabilir ama reddetmemeli — ya da bildirimin ayrı bir bütçesi
    olmalı; aksi hâlde karar 2'nin gerekçesi düşer.** Aynı yön, ikinci yol: SES hesabın
    gönderimini duraklatırsa (ana listenin sınır 6'sı) bildirim de durur; o yolun önlemi kodda
    değil, hesap itibarındadır.

**EM-9 2. tur (2026-10-07 — üçüncü göz RED, bir bloklayan METİN bulgusu; yalnız test + metin +
pin, ürün kodunun davranışı değişmedi — `.go` üretim dosyalarında yalnız yorum):**
- **[ORTA, bloklayan] Tehdit modeli cümlesi** `web/templates/email/email.go`'nun iddia bloğunda
  yoktu (EM-9 o bloğa madde eklemişti) → paket için uyarlanmış cümle eklendi.
- **M02** (`Submit`'in `default:` dalı da bildirir) YEŞİLDİ → kurtarma testine *"bizim tarafımızda
  başarısız harcama"* satırı (500, bildirim 0, satır 0) → KIRMIZI.
- **M26a/M26b** (tavan penceresi 1 saat → 1 s, tavan 5 → 30) YEŞİLDİ: test sınırlayıcıyı aynı
  sabitlerle karşılaştırıp kendini doğruluyordu → sabitler literal değere (5, 1 saat, 10 s, 5 s)
  ve bu notun cümlelerine (ADR'den okunur, sabitten biçimlenir) bağlandı → KIRMIZI.
- **M10** (satır yazımının kendi süresi yok) ve **M35** (adres okuması gönderim süresinin dışında)
  YEŞİLDİ → `TestPasswordNotice_EachStepHasItsOwnBound` (asılı okuyucu ve asılı kayıt yazıcısı) →
  ikisi de KIRMIZI.
- **M40** (`main.go` panele kanalsız ikinci bir akış verir) YEŞİLDİ → kaynak pini
  `TestPasswordNoticeWiring_ThePanelIsGivenTheMountedRecoveryFlow` → KIRMIZI; ek varyant M40b
  (tek akış `nil` kanalla) da KIRMIZI. Sınır 10 pinin kapsamını sayar.
- **Metin:** test çağrısı sayıları düzeltildi (yukarıda); kartın karar numaraları bu notun
  numaralarına hizalandı.
- Betik `scratchpad/em9/verify_round2.py` (yalnız yolunda `em9-orch` geçen kopyada koşar; her
  mutasyondan önce mutasyonsuz kontrol yeşil; çapa tam bir kez; sha256 ile geri yükleme;
  `BUILD-FAILED` ayrı): 7/7 KIRMIZI, 7/7 `sha-ok`. M40b'nin ilk yazımı derlenmedi
  (`resetChannel` kullanılmaz kalıyordu) ve `BUILD-FAILED` olarak ayrıldı; yeniden yazıldı.

**EM-9 3. tur (2026-10-07 — üçüncü göz ve güvenlik ONAY; yalnız metin + rebase, ürün kodu ve
testlerin davranışı değişmedi):**
- **Rebase:** değişiklik dal ucu `20d62ab`'ye (EM-6 commit'li) taşındı. Bu ADR'de EM-6 notu ile
  bu not aynı bölgede çakıştı; ikisi de korundu — EM-6 notu önce, bu not sonra. EM-6'nın metni
  bayt bayt aynı (EM-9'un eklemeleri çıkarılınca dosya `20d62ab`'ninkiyle sha256-eşit; EM-6 notu
  bölümü tek başına da eşit — kanıt betiği `scratchpad/em9/round3/adrproof.py`). EM-6'nın iki testi
  (`employeeemail_test.go`, `employeeemail_db_test.go`) `NewAdminAuth`'u bildiricisiz çağırıyordu
  → `&fakeNotices{}` eklendi; test çağrıları artık **36** / **27** dosya (33'ü `&fakeNotices{}`,
  1'i gerçek akış, 2'si yeni testlerin kendi bildiricisi).
- **[ORTA] §9 kesicisi bildirimi susturabilir** → sınır 11, EM-7 devri, karar 2'ye işaret,
  *Karar verilmedi*'deki kesici maddesine işaret, ana listenin sınır 6'sına (SES duraklatması)
  bildirim, `passwordnotice.go`'nun karar yorumuna kısa işaret (yalnız yorum).
- **[DÜŞÜK] (b) önkoşulunun kapsamı:** EM-5A notunun *Devirler (b)* maddesine *"(EM-9: bildirimi de
  kapsar — EM-9 notu, sınır 3)"*.

**Devirler:**
- **EM-5B:** gerçek SES ile bildirim de ölçülür (SPF/DKIM/DMARC, message-id, izleme kapalı,
  linkin `taptime.mt` giriş sayfası olduğu); ConfigMap'i `email`'e çevirmek bildirimi de açar —
  önkoşul (b) (alıcı başına tavan kararı ya da açık risk kabulü) bu iletiyi de kapsar.
- **EM-7:** bildirimde ad gösterilip gösterilmeyeceği EM-7'nin ürün kararına bağlıdır (bugün
  yok); §9 devre kesicisi bildirimi de sayar — **kesici bildirimi sayabilir ama reddetmemeli — ya
  da bildirimin ayrı bir bütçesi olmalı; aksi hâlde karar 2'nin gerekçesi düşer** (sınır 11;
  kurtarma formu tek kaynaktan kesiciyi açabilir).
- **Yönetici adresini değiştiren gelecekteki görev** (bugün kartı yok; EM-6 çalışan adresidir):
  sınır 6.
