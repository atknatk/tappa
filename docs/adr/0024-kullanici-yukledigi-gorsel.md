# ADR 0024 — Kullanıcının yüklediği görsel (tenant logosu): PNG/JPEG, decode'dan önce sınır, yeniden kodlama, `bytea`, oturumdan tenant

- **Durum:** kabul edildi — White-label **K5** (PNG/JPEG) ve **K7** (Postgres `bytea`),
  *"✅ önerisiyle uygulanır"* ([m10-platform.md](../plan/m10-platform.md) §6); tap ekranındaki
  logonun kendisi kullanıcı kararı **D-C** (2026-09-24, [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) §7).
  **Uygulama: yok** (HEAD `c0c0250`'de üretim kodunda görsel paketi import eden, multipart
  okuyan ya da şablonda `<img>` render eden yer 0 — aşağıda ölçüldü); uygulama WL-1, WL-3,
  WL-4, WL-6, WL-7, denetim WL-10. **WL-3 (2026-10-03):** `internal/brand/logo.go` ve
  `logo_resize.go` — biçim kapıları, sınırlar, tarama tavanı, semafor, kutu filtresi,
  yeniden kodlama; tavan, süre ve bellek ölçümleri sondaki *"WL-3 notu"*nda. **WL-6
  (2026-10-03):** `internal/handler/brandlogo.go` ve `internal/domain/tenant/brandread.go` —
  iki logo rotası, sayfa politikasının img-src yarısı, `hasLogo` türetimi; kararlar, ölçümler
  ve İddia D/E'nin WL-6 parçaları sondaki *"WL-6 notu"*nda.
- **Tarih:** 2026-10-02
- **Bağlam:** [M10 Akış C](../plan/m10-platform.md) §5, görev WL-0. Sapmalar ve ölçüm komutları:
  aynı dosya → *"Kart düzeltmesi (2026-10-02, WL-0 uygulaması sırasında)"*.
- **İlgili:** [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) (slotlar; logonun nerede
  göründüğü) · [ADR 0002](0002-tenant-baglami-ve-rls.md) md.7 ·
  [ADR 0005](0005-kabul-edilen-riskler.md) · CLAUDE.md §1 (yeni bağımlılık), §4.1, §4.2, §4.5,
  §4.6, §6, §7

**Numara notu.** 0022 SES ADR'sine (EM-1) ayrılmış kalır; 0023 ve 0024 ondan önce yazıldı.
Akış sırası A1 → C → B → A2: kullanıcının *"çok yavaş, paralel ilerle"* talebi üzerine, SES'in
dış adımları kullanıcı tarafından sonraya bırakıldığı için orkestratörün önerisi (otonomi
kuralı); 2026-10-02'de kullanıcıya raporlandı, itiraz yok. Numaralar değişmez (ADR 0023
"Numara notu").

## Neden bir ADR

HEAD'de kullanıcının yüklediği bir dosyayı okuyan kod yok (Bağlam: multipart 0, görsel paketi
0); logo ile ürün **kullanıcının verdiği ikili bir içeriği saklayıp başka kullanıcılara
sunmaya** başlar. Bu üç saldırı yüzeyi açar: tarayıcıya giden içerik (betik, koklama, çok-biçimli
dosya), sunucunun kendi kaynakları (bellek ve CPU bombası, geçici dosya) ve tenant sınırı
(başka tenant'ın logosu). Ayrıca iki kırmızı çizgiye değer: telefon fotoğrafının EXIF'i konum
taşır (§4.2) ve logo bir kişinin fotoğrafı olabilir (§4.1). CLAUDE.md §10: şema ve güvenlik
sınırı değişiyor.

## Bağlam — bugün (ölçüldü, HEAD `c0c0250`)

| Olgu | Kaynak / ölçüm |
|---|---|
| Üretim Go kodunda `image/png`, `image/jpeg`, `image/gif` import eden dosya **0** | `rg -l '"image/(png\|jpeg\|gif)"' --glob '*.go' --glob '!*_test.go' internal cmd web` |
| Üretim Go kodunda `.FormFile(`, `.MultipartReader(`, `.ParseMultipartForm(` **0** | `rg -n '\.(FormFile\|MultipartReader\|ParseMultipartForm)\(' --glob '!*_test.go' internal cmd web` → çıkış 1 |
| Landing SSS'inin *"dosya içe aktarma yok"* cevabı bu yokluğa bağlı: `FactNoBulkImport`, deseni `\.(?:FormFile\|MultipartReader\|ParseMultipartForm)\(` | `web/templates/pages/landingview.go:297`, `:869`; `internal/handler/marketing_facts_test.go:48`, `:274-281`; koşan test `TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` |
| `go.mod`/`go.sum`'da `golang.org/x/image` **0** | `rg 'golang.org/x/image' go.mod go.sum` |
| Mevcut form POST'ları gövdeyi ayrıştırmadan önce `http.MaxBytesReader` ile sınırlar (emsal; ölçülen örnekler) | `internal/handler/accountpassword.go:85`, `policyactions.go:410`, `checkin.go:169` |
| Go 1.27.1 stdlib'inde EXIF okuyucu yok: `rg -il exif $GOROOT/src --glob '*.go'` → 1 dosya (`image/jpeg/reader.go`), isabetlerin dördü de bir URL yorumu | sonda |
| Go 1.27.1 stdlib'inin görsel çözücüleri `image/gif`, `image/jpeg`, `image/png`; SVG ya da WebP paketi yok (`$GOROOT/src` altında adı `svg`/`webp` içeren dizin 1: `cmd/vendor/…/pprof/third_party/svgpan`, içe aktarılabilir bir stdlib paketi değil) | `ls $GOROOT/src/image`; `find $GOROOT/src -type d -iname '*svg*' -o -iname '*webp*'` |
| `http.Server`'da `ReadHeaderTimeout: 10s` ve `IdleTimeout: 90s` var; `ReadTimeout`/`WriteTimeout` **yok** | `cmd/tappa/main.go:655-656` |
| Router'da `middleware.Timeout(30 * time.Second)` (chi v5.3.1): isteğin **context'ine** 30 s süre koyar ve 504'ü handler **döndükten sonra** ertelenmiş çağrıda yazar; handler'ı kesmez. `image/png` ve `image/jpeg` çözücüleri context almaz — 30 s bir decode'u durdurmaz | `internal/httpx/router.go:83`; `go.mod` chi `v5.3.1`; chi `middleware/timeout.go:32-47` (modül önbelleğinde okundu) |
| Pod: `readOnlyRootFilesystem: true`, uygulama konteynerinde volume **yok**, bellek sınırı **512Mi**, CPU sınırı **2**, **tek replika** | `deploy/k8s/20-app.yaml:493`, `:497-503`, `:53`; `rg -n 'volumes\|volumeMounts\|emptyDir' deploy/k8s/20-app.yaml` → 0 |
| Depoda `GOMEMLIMIT` / `GOGC` / `SetMemoryLimit` / `SetGCPercent` ayarı **0** (tek `rg` isabeti `scripts/rotate-kek.sh:271`'de `GOGCCFLAGS` geçen bir yorum) | `rg -n 'GOMEMLIMIT\|GOGC\|SetMemoryLimit\|SetGCPercent' --glob '!*.md' .` |
| Ingress `proxy-body-size: "1m"`; yorumu *"Every request this product accepts is a form post or a small JSON body"* diyor — logo yüklemesiyle bu önkabul güncelliğini yitirir. İstek tamponlama için annotation yok (ingress-nginx varsayılanı; kümede **ölçülmedi**) | `deploy/k8s/40-ingress.yaml:127-134` (OP-10B'den beri; yorum 127-133, ayar 134) |
| Bütçeler: panel oturumu 300 / 10 dk, adres 3000 / 10 dk, kayıt 3 tenant / saat / adres | `internal/handler/adminratelimit.go:595-596`, `:152-153`; `internal/handler/signupratelimit.go:145-146` |
| `r.FormValue`, `r.PostFormValue` ve `r.FormFile` multipart gövdede örtük olarak `ParseMultipartForm(32 MiB)` çağırır; `PostFormValue` üretimde kullanılıyor | Go 1.27.1 `net/http/request.go:37`, `:1442-1474`; `internal/handler/adminlogin.go:880-881` |
| `ProtectWriting` zinciri (`floodGate → sameOriginGate → requireAdmin → sessionGate`) gövde okumuyor: aynı-origin kapısı `Origin` ve `Sec-Fetch-Site` başlıklarını okur; `internal/httpx`'te form okuyucu çağrısı 0 | `internal/handler/adminlogin.go:680-687`, `:1556-1565`; `rg -n 'Form' internal/httpx --glob '!*_test.go'` (yorum dışı 0) |
| Go'nun JPEG çözücüsü segmentler arasındaki fazla baytı **sessizce atlayıp** bir sonraki `FF xx` işaretine hizalanır ve kaynağı 4096 baytlık `Read`'lerle çeker | Go 1.27.1 `image/jpeg/reader.go:542-568`; `:116`, `:170` |
| Kümede düğüm dışı depolama yok: tek düğüm, tek StorageClass (`local-path`), CSI sürücüsü yok; yedek `pg_dump` | `deploy/k8s/50-backup.yaml:29-33` |
| Yönetici çerezi `Path=/admin`, çalışan oturum çerezi `Path=/`. Çerez yolu kuralıyla: yönetici çerezi `/admin` dışındaki bir yola (ör. `/t/…`) gönderilmez; çalışan çerezi `/admin/…` dahil her yola gönderilir. Yönetici çerezinin tap yüzeyine ulaşmaması bir testle pinli | `internal/adminauth/cookie.go:48`; `internal/session/cookie.go:182`; `TestPanelCookies_NeverReachTheTapSurface`, `TestPanelCookiePath_IsNarrowerThanTheTapSurface` |
| Hesap yazma kapısı (owner + canlı oturum), audit'in tx içi kaydı | `internal/handler/accountactions.go:95-97`; `internal/audit/audit.go:110-119` (`RecordTx`) |
| Panel kabuğu, sayım okuması düşünce sayfayı düşürmez (`PendingBadge`, §4.6 emsali) | `internal/handler/review.go:527-555` |
| Şablonlarda `<img` **0**; yedi sayfa CSP'sinde `img-src` yalnız `landingCSPFor`'da (poster varken) | ADR 0023 Bağlam |
| `/static` düz `http.FileServer`, `Cache-Control`/`nosniff` eklemez | `internal/httpx/router.go:111` (ayrı sertleştirme işi — plan §7) |

**OP-8 sonrası (`71272fa`) — 2026-10-02 notu.** Tablo `c0c0250`'yi sabitler; dalın ucunda
(`git show 71272fa:…` ile okundu) `router.go:83` (`middleware.Timeout(30 s)`) yerinde, `/static`
`:111` → `:118`. İki operatör politikası eklendi (`operatorCSP`, `enrollCSP()` —
`internal/handler/operator/render.go:28-29`, `:39-41`); ikisi de `img-src` taşımaz, yani dalın
ucundaki dokuz politikada `img-src` hâlâ yalnız `landingCSPFor`'dadır (`marketing.go:506`).

### Ölçülen standart kütüphane davranışı

Sonda: yalnız stdlib, scratchpad'de ayrı modül; **go1.27.1 darwin/amd64**. Depo `go 1.26.2`
ister ve CI 1.26.6 koşar (`ci.yml:91`); bu sayılar CI araç zincirinde yeniden ölçülmedi.
*(WL-3 düzeltmesi, 2026-10-03: burada "WL-3'ün testleri onları CI'da yeniden üretir"
yazıyordu — tutmuyor. CI yalnız `go test -race -count=1 ./...` koşar (`Makefile:248`,
`ci.yml:200-201`) ve S7–S9'un sayılarını üreten üç ölçüm testi `-race` altında atlanır;
CI'da koşan, WL-3'ün kabul pinleridir. Sayılar `-race`'siz yerel koşudan — WL-3 notu, sayılı
sınır 2.)* Fikstürler ImageMagick ve elle kurulmuş başlıklarla üretildi.

| # | Girdi | Ölçülen |
|---|---|---|
| S1 | `http.DetectContentType`: PNG · JPEG · GIF · WebP · çıplak SVG · XML önsözlü SVG · HTML · BMP · PDF | `image/png` · `image/jpeg` · `image/gif` · `image/webp` · `text/plain; charset=utf-8` · `text/xml; charset=utf-8` · `text/html; charset=utf-8` · `image/bmp` · `application/pdf` |
| S2 | PNG imzası + HTML · JPEG imzası (`FF D8 FF`) + HTML | koklama **`image/png`** · **`image/jpeg`** der (geçer); `png.DecodeConfig` → `unexpected EOF`, `jpeg.DecodeConfig` → `unknown marker` (red) |
| S3 | 75 baytlık PNG, IHDR 30000×30000 | `png.DecodeConfig` 30000×30000 döner, **1 088 B** ayırır |
| S4 | 75 baytlık PNG, IHDR 8192×8192, 5 baytlık IDAT | `png.Decode` hata vermeden önce **256,13 MiB** ayırır (`not enough pixel data`) |
| S5 | 139 baytlık JPEG, SOF0 30000×30000 | `jpeg.DecodeConfig` döner, **13 616 B** |
| S6 | 139 baytlık JPEG, SOF0 8192×8192, kesik tarama | `jpeg.Decode` hata vermeden önce **64,01 MiB** ayırır |
| S7 | 2048×2048 gerçek dosyalar, `image.Decode` başına `TotalAlloc` | gri JPEG 4,02 MiB · 4:2:0 JPEG 6,02 · 4:4:4 JPEG 12,02 · CMYK JPEG 26,02 · paletli PNG 4,14 · gri PNG 4,15 · RGBA PNG 16,35 · **interlaced RGBA PNG 32,45** · **16-bit RGBA PNG 38,13** · **progressive 4:4:4 JPEG 60,02** · **16-bit RGBA interlaced PNG 64,22** (düz içerik, 111 253 B — 512 KiB'a sığar) · **progressive CMYK 66,02** · 16-bit gri+alfa interlaced PNG 67,60 (8,6 MB) · 16-bit RGBA interlaced PNG 70,13 (gürültülü, 21 MB — ikisi de 512 KiB'a sığmaz) · **progressive CMYK 4:4:4 96,02 MiB** |
| S8 | Aynı dosyalar, decode süresi | 30–425 ms (16-bit PNG 425 ms, progressive CMYK 4:4:4 333 ms) |
| S9 | Elle kurulmuş progressive JPEG, 2048×2048, tek bileşen, N AC taraması, her tarama EOB koşusuyla | N=10 → 45 ms · 100 → 256 ms · 1 000 → **2,72 s** · 3 000 → **8,72 s** (56 333 B); bellek sabit 20,01 MiB. Tarama başına **16 B**; 512 KiB'a **32 247** tarama sığar → doğrusal kestirimle **~94 s** CPU (kestirim, ölçülmedi) |
| S10 | Standart kodlayıcıların gerçek progressive çıktısı | `magick … -interlace JPEG`: 3 bileşen **10** tarama, CMYK **18** tarama; libjpeg-turbo 3.2.0 `jpegtran -scans` betikte **100**'den fazla taramayı reddediyor (*"Too many scans"*). Go'nun `image/jpeg`'inde tarama sayısı sınırı **yok** (`scan.go` okundu) |
| S11 | PNG + IEND'den sonra 38 bayt HTML | `png.Decode` hatasız; yeniden kodlanmış çıktı `IHDR IDAT IEND`, `<script` yok |
| S12 | PNG + `eXIf` (GPS IFD'li TIFF) + `tEXt` (`<script>`) + `iCCP` | decode hatasız; çıktı `IHDR IDAT IEND`; TIFF imzası da `<script` de yok |
| S13 | JPEG + APP1 `Exif` (GPS IFD'li) + `COM` (`<script>`) + EOI'den sonra HTML | decode hatasız; çıktı `SOI DQT SOF0 DHT SOS EOI`; `Exif` yok, `<script` yok |
| S14 | Standart kodlayıcıların yazdığı bölümler (girdiler: NRGBA 8×8, paletli+alfa 8×8, S11–S13'ün çıktıları) | `png.Encode`: `IHDR IDAT IEND` (paletli + alfa: `IHDR PLTE tRNS IDAT IEND`); `jpeg.Encode`: `SOI DQT SOF0 DHT SOS EOI` — bu girdilerde APPn segmenti yok |
| S15 | Yarıda kesilmiş PNG · JPEG · EOI'siz JPEG | `not enough pixel data` · `short Huffman data` · `unexpected EOF` |
| S16 | 16×16 PNG, IDAT'ı 100 MiB sıfıra açılıyor (205 646 B) | `too much pixel data`, **0,04 MiB**, 1 ms |
| S17 | `multipart.Reader.ReadForm(64 KiB)`, 600 KiB dosya parçası, `TMPDIR` yok | `open /nonexistent-…/multipart-…: no such file or directory` (geçici dosya açmaya çalışıyor) |
| S18 | Aynı gövde, `NextPart` + `io.LimitReader(512 KiB + 1)` | 524 289 bayt okundu, hata yok, geçici dosya yok — aşım `n > 512 KiB` ile görülür |
| S19 | 512×512 fotoğraf benzeri PNG → PNG `BestCompression` · aynı görüntü JPEG q85 · düz bir logo | **440,0 KiB** (256 KiB'ı aşar) · 43 KiB · 8,5 KiB |
| S20 | `MaxBytesReader(1 MiB)` altında 600 KiB'lık `logo` parçası, `TMPDIR` yok: `r.FormFile` · `r.FormValue` · `r.PostFormValue` · `ParseMultipartForm(1 MiB)` · `ParseMultipartForm(64 KiB)` · `MultipartReader` + `LimitReader` · 1 100 KiB parçayla `FormFile` | hata yok, 614 400 B · çok parçalı formu ayrıştırdı (1 dosya) · ayrıştırdı · hata yok · **geçici dosya açamadı** · 524 289 B, hata yok · `http: request body too large`. Sonuç: 1 MiB gövde tavanı altında `FormFile`/`FormValue`/`PostFormValue` 32 MiB eşiğinin altında kalır ve **diske dökmez** — `TMPDIR` olmadan çalışmaları onları `MultipartReader`'dan ayırmaz |

## Karar

### 1. Biçim: PNG ve JPEG; tespit içerikten (K5)

Kabul edilen biçim kümesi **{PNG, JPEG}**. Bir dosya ancak üç kapının üçünden geçerse logo olur
(→ **WL-3**):

1. `http.DetectContentType(ilk 512 bayt)` ∈ {`image/png`, `image/jpeg`}.
2. Koklanan biçimin **kendi** `DecodeConfig`'i (`png.DecodeConfig` / `jpeg.DecodeConfig`)
   hatasız döner.
3. Decode edilen biçim koklanan biçime eşittir.

İstemcinin `Content-Type`'ı, dosya adı ve uzantısı **okunmaz ve loglanmaz.**

Gerekçe, ölçüyle: koklama tek başına imzayla başlayan bir HTML'i geçirir (S2) — ikinci kapı onu
reddeder (S2). Decode edilebilen bir çok-biçimli dosyanın ek yükü S11–S13'te ölçülen biçimlerde
(IEND/EOI sonrası bayt, `tEXt`, `eXIf`, `iCCP`, `COM`, APP1) yeniden kodlamada düştü (§3).
Piksel verisinin kendisine gömülü bir yük yeniden kodlamadan sağ çıkabilir; logo yanıtının
tarayıcıda belge olarak yorumlanmasına karşı asıl kontrol İddia E'nin başlıklarıdır
(`nosniff`, `sandbox`, CORP, sabit `Content-Type`).

**Elenen biçimler:** SVG — betik taşır; logo URL'i doğrudan açıldığında betik **bizim
origin'imizde** koşar, stdlib'de güvenli bir SVG temizleyicisi yok; ve koklama onu metin olarak
görür (S1). GIF — animasyon. WebP — stdlib'de çözücü yok, `golang.org/x/image` yeni bağımlılık
olurdu (CLAUDE.md §1: kullanıcıya sorulur; bugün `go.mod`'da yok). BMP, TIFF, HEIC — kapsam
dışı. iPhone kamerasının varsayılanı HEIC'tir; tarayıcının dosya seçicisinin onu JPEG'e
çevirip çevirmediği **ölçülmedi** (→ WL-7 gerçek cihaz turu).

### 2. Sınırlar — decode'dan önce, bu sırayla

S4 ve S6 kuralın sebebidir: çözücü **başlıktaki** boyuta göre ayırır, veriyi doğrulamadan
önce. 75 baytlık bir PNG 256 MiB ayırttı.

1. **Gövde:** `http.MaxBytesReader` **1 MiB** (→ **WL-7**).
2. **Tek parça:** `r.MultipartReader()` ile akış; tam olarak **bir** parça, adı `logo`;
   bilinmeyen ya da ikinci parça → red. Parça `io.LimitReader(512 KiB + 1)` ile okunur,
   **512 KiB**'ı aşan → red (S18) (→ **WL-7**).
3. **Biçim kapıları** (§1).
4. **Boyut:** `DecodeConfig`'in döndürdüğü her kenar **16–2048 px**; piksel sayısı
   **≤ 4 194 304** (= 2048²). Planın *"≤4 MP"*'si 2²² okunur: kenar sınırıyla aynı kümeyi
   tanımlar (→ **WL-3**: 30000×30000 başlıklı dosya decode'dan önce red, tahsis ölçülür — S3/S5
   ölçeğinde).
5. **🔴 JPEG tarama sayısı — tasarım özünde yoktu, S9 ekledi.** Go'nun çözücüsü tarama sayısını
   sınırlamaz (S10) ve her tarama her blok için 256 baytlık katsayı bloğunu okuyup yazar; 512
   KiB içinde ~32 000 tarama sığar. Sunucuda `WriteTimeout` yok; router'ın 30 s'lik süresi
   yalnız context'e konur ve çözücü context okumaz (Bağlam). Sonuç: tek bir yükleme bir
   çekirdeği ~94 s tutabilir (doğrusal kestirim, S9); ve kayıt herkese açık olduğu için (ADR
   0013) yükleme yetkisi olan bir owner yaratmak bir kayıt kadar ucuzdur.
   **Kural:** SOS sayısı decode'dan önce sayılır ve bir tavanı aşarsa red. **Sayım, çözücünün
   işleyeceği SOS sayısının üst sınırı olmak zorundadır.** Gerekçe: Go'nun çözücüsü segmentler
   arası fazla baytı atlayıp bir sonraki işarete hizalanır (Bağlam, `reader.go:542-568`);
   segment uzunluklarını izleyerek ilerleyen bir ön tarayıcı, uzunluğu yalan söyleyen bir
   segmentin içine ya da segmentler arası "çöpe" gizlenmiş bir SOS'u atlar ve çözücüden **az**
   sayar. Üst sınırı veren iki biçim: (a) ham bayt akışındaki her `FF DA` çiftini saymak —
   çözücünün işlediği her işaret ham akışta bir `FF` ve ardından işaret baytıdır (dolgu
   `FF`'leri dahil); APPn içindeki bir `FF DA` fazladan sayılır, bu yönde hata reddetmektir;
   (b) çözücüye verilen okuyucuyu sarmalayıp her `Read`'de aynı sayımı ve bir süre denetimini
   yapmak (çözücü 4096 baytlık `Read`'lerle çeker). Tavan **WL-3**'te ölçümle konur; iki sınır
   ölçüldü: alt sınır **18** (S10'da ölçülen en çok taramalı kodlayıcı çıktısı —
   ImageMagick/libjpeg-turbo, CMYK), üst sınır çözme süresini bütçede tutan değer — doğal aday
   router'ın **30 s**'si (S9: 2048² tek bileşende tarama başına ~2,9 ms, bu makinede; N yuvayla
   birlikte hesaplanır). (→ **WL-3** kabulü: tavanın bir fazlası taramalı elle kurulmuş dosya
   decode'dan önce red; **"dürüst olmayan" dosya** — segmentler arası çöp ve uzunluk alanının
   içine gizlenmiş SOS — da red; tavan kadar taramalı dosyanın çözme süresi ölçülüp karta
   yazılır.)
6. **Süreç geneli semafor**, **N** yuva (→ **WL-3**):
   - **Kapsam (yuvanın ne zaman alınıp bırakıldığı):** yuva decode'dan önce alınır ve tam
     boyutlu görüntüye erişim — decode → küçültme → kodlama — bitene kadar tutulur. Yuva
     **decode eden goroutine döndüğünde** bırakılır, context iptal edildiğinde değil: çözücü
     context okumadığı için iptalde bırakılan yuva arkasında süren bir decode bırakır ve yeni
     istek yeni yuva alır (`WriteTimeout` yok; ingress'in okuma süresi için annotation yok —
     ingress-nginx varsayılanı, kümede ölçülmedi).
   - **Alma politikası — tek: beklemeden dene, doluysa hemen ret** ("tekrar dene"; decode
     başlamaz). Bekleme yok, kuyruk yok. Gerekçe: bekleyen her istek okunmuş gövdesini (≤1 MiB)
     ve goroutine'ini tutar; ve semaforun önündeki kabul sınırı (§6) eşzamanlı yüklemeyi zaten
     gövde okunmadan sınırlar. Kabul: `-race` altında eşzamanlı çözme ≤ N; N yuva doluyken gelen
     istek decode'a girmeden hemen reddedilir; context iptal edilen bir istekten sonra yuva,
     decode bitene kadar dolu görünür.
   - **N'nin hesabı düzeltildi:** tasarım özü *"en kötü 16 MiB RGBA"* diyordu; ölçülen en kötü
     **96,02 MiB** (progressive CMYK 4:4:4, S7) — altı katı. `TotalAlloc` canlı heap'in üst
     sınırıdır. GC payı: depoda `GOMEMLIMIT`/`GOGC` ayarı yok (Bağlam); `GOGC=100`'de heap hedefi
     canlı heap'in yaklaşık iki katıdır. Bu yüzden ya `GOMEMLIMIT` konur ya da sınır
     `2 × (N × tepe + taban) < 512Mi` ile hesaplanır. Kabul: WL-3, 512Mi sınırlı bir konteynerde N
     eşzamanlı en kötü decode altında **RSS**'i ölçer ve karta yazar. *(WL-3 düzeltmesi: bir
     konteynerde ölçüldü — WL-3'ün üçüncü gözü, go1.26.6 linux/amd64, `--memory=512m
     --cpus=2`, ürünün kendi tabanı dahil değil; sayılar WL-3 notunda. Podda ürün tabanıyla RSS
     ölçülmedi — deploy'da orkestratörün.)* Tek replika, yani OOMKill tap dahil ürünün durması
     demektir (`20-app.yaml:53`).
   - **CPU — kural ve sonucu:** N < `GOMAXPROCS` (bir yuva bir çekirdeği tutar; en az bir
     çekirdek tap yoluna kalmalı). `GOMAXPROCS`'un kaynağı: Go 1.25 ve sonrasının çalışma zamanı
     Linux'ta cgroup CPU sınırını okur ve varsayılanı mantıksal CPU sayısı ile o sınırın küçüğüne
     indirir; `go.mod` `go 1.26.2` (davranış varsayılan açık; depoda `GOMAXPROCS` ayarı ve
     `godebug` yönergesi 0). Düğüm 16 CPU (`10-postgres.yaml:208`), sınır `"2"`
     (`20-app.yaml:502`) → `GOMAXPROCS` = 2 (türetildi, podda ölçülmedi) → **N = 1 kuralın
     sonucudur**, öneri değil. Kabul: WL-3 pod içinde `runtime.GOMAXPROCS(0)`'ı bir kez ölçer ve
     karta yazar; 2'den farklıysa N yeniden hesaplanır. *(WL-3 düzeltmesi: `--cpus=2`
     sınırlı bir konteynerde ölçüldü — `GOMAXPROCS` = 2 (NumCPU 4, `cpu.max` "200000 100000"),
     türetme tutuyor; podun kendisinde ölçülmedi — deploy'da orkestratörün.)*
   - **Alternatif** (WL-3'ün seçimi, gerekçesiyle): progressive ya da 4 bileşenli JPEG'i
     reddetmek tepeyi 96'dan **~64 MiB**'a indirir — 1,5 kat; kalan en kötü 16-bit RGBA
     interlaced PNG'dir (S7: 512 KiB'a sığan düz örnekte 64,22 MiB). Bedeli, tasarımcıların sık
     verdiği CMYK baskı logolarıdır.

### 3. Yeniden kodlama — saklanan bayt, yüklenen bayt değildir

Kabul edilen her görsel decode edilir, uzun kenarı **≤512 px**'e elle yazılmış bir kutu
filtresiyle küçültülür (stdlib; `x/image/draw` bağımlılık olurdu) ve **yeniden kodlanır**:
PNG → PNG (`BestCompression`), JPEG → JPEG (q85). Saklanan ve sunulan tek şey bu çıktıdır.

- S14'te ölçülen girdilerde standart kodlayıcılar S14'teki bölümleri yazdı; S11–S13'te
  ölçülen EXIF (GPS IFD'li), `eXIf`, `tEXt`, `iCCP`, `COM`, APP1 ve IEND/EOI sonrası bayt
  **çıktıya geçmedi** (→ **WL-3**: IEND sonrası yük çıktıda yok; EXIF-GPS'li JPEG'in
  çıktısında `Exif` APP1 yok; CMYK JPEG, 16-bit, paletli, interlaced PNG normalize olur;
  çıktının bölüm listesi S14'teki kümedir).
- **§4.2:** telefon fotoğrafının EXIF'indeki GPS, tap anı dışında kaydedilmiş bir konumdur.
  Ürün EXIF'i **ayrıştırmaz** (Go 1.27.1 stdlib'inde EXIF okuyucu yok — Bağlam; bu ADR
  eklemez) ve yeniden kodlama onu düşürür: S12 ve S13'ün GPS IFD'li girdilerinde konum
  çıktıya geçmedi.
- **Çıktı ≤ 256 KiB**, aşan → *"logoyu sadeleştir"* reddi. Ölçülen bedel (S19): fotoğraf benzeri
  512 px bir PNG 440 KiB'a kodlanır ve reddedilir; aynı görüntü JPEG olarak 43 KiB'tır. WL-7'nin
  ret cümlesi JPEG'i önerir.
- Her başarılı çıktı saklanmadan önce bir kez daha decode edilir ve §2'nin sınırlarındadır
  (→ **WL-3**: fuzz testi — panik yok, her başarılı çıktı yeniden decode olur ve sınırlarda).
- **Bedeller (sayılı):** EXIF `Orientation` uygulanmaz — telefonla çekilmiş bir logo yan dönük
  görünebilir; ICC düşer — geniş gamlı ya da CMYK bir görüntünün rengi kayabilir. Editörün
  önizlemesi (WL-7) yeniden kodlanmış hâli gösterir.

### 4. Saklama — Postgres `bytea`, sha256 adresli (K7)

- `tenant_branding.logo bytea` (≤ 262 144 bayt CHECK), `logo_sha256` (çıktının sha256'sı, küçük
  harf hex), `logo_mime IN ('image/png','image/jpeg')`, `logo_width/height` 1–512; logo alanları
  hep-birlikte-dolu ya da hep-birlikte-boş CHECK (→ **WL-1**: CHECK'ler hasmane değerlerle
  patlatılır — 262 145 bayt, kısmi logo alanları).
- `GetTenantBrand` logo baytlarını **seçmez** (sayfa başına okunan küçük satır); baytlar yalnız
  `GetTenantLogo(tenant_id, sha)` ile okunur; ikisi de açık `tenant_id` filtreli (§4.5) (→
  **WL-1**: test sorgu metnini okur).
- Neden `bytea`: kök dosya sistemi salt-okunur ve volume yok; kümede obje deposu yok
  (`50-backup.yaml:29-33`); `pg_dump` yedeğine kendiliğinden girer; 256 KiB × 1 000 tenant =
  250 MiB (plan *"≈ 256 MB"* yazıyordu — 262 MB ondalık), TOAST satır dışında tutar. Dış CDN elendi: üçüncü taraf URL'i (CSP ve izleme).
- sha256 bir **yetki değildir**: tenant her istekte oturumdan gelir (§5). İki tenant aynı
  baytları yüklerse aynı sha'yı taşır; her birinin sorgusu kendi `tenant_id`'sine filtrelidir
  (§5, İddia D).

### 5. Servis

- **İki rota:** `GET /admin/brand/logo/{sha}` (panel okuma zinciri, yönetici çerezi) ve
  `GET /t/logo/{sha}` (tap grubu, canlı çalışan oturumu şart). Gerekçe: yönetici çerezi
  `/admin` dışına gönderilmez, yani tap yüzeyindeki bir rota yönetici kimliğini göremez; ve iki
  yüzeyin kimlik çözümleyicisi ve bütçesi ayrıdır. **Tasarım özünün gerekçesi düzeltildi:**
  *"tek ortak rota iki çerezi alamaz"* tutmuyor — çalışan çerezi `Path=/` olduğu için
  `/admin/…` altındaki bir rotaya da gönderilir (Bağlam). Her rota yalnız kendi yüzeyinin
  çözümleyicisini kullanır; `/admin/brand/logo/…` çalışan oturumunu kimlik saymaz (→ **WL-6**:
  yönetici oturumu olmayan, çalışan çerezli istek `/admin/brand/logo/…`'ya 404 ya da panelin
  oturumsuz yanıtını alır, logo baytını almaz).
- **Tenant yalnız oturumdan.** Başka tenant'ın sha'sı ve var olmayan sha **bayt-aynı 404**
  alır — gövde, durum ve başlıklar (→ **WL-6**: A oturumu B'nin sha'sını isteyince aldığı
  404, bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu 404). *(WL-6 düzeltmesi,
  2026-10-03: bu 404 tek bir yazıcıdır — `Content-Type: text/plain; charset=utf-8`,
  `Content-Length`, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, gövde
  `Not found.\n`; `no-store` çünkü aynı URL logoyu tutan oturuma 200 verir. Rotanın
  eşleştiği yollarda geçerlidir; rotayla eşleşmeyen yol şekilleri yönlendiricinin kendi
  404'ünü alır — WL-6 notu, karar 4 ve sınır 3.)*
- **Başlıklar** (→ **WL-6**: birebir):
  `Content-Type` = saklanan mime (sunum anında koklanmaz) · `X-Content-Type-Options: nosniff` ·
  `Content-Security-Policy: default-src 'none'; sandbox` (URL doğrudan açılırsa) ·
  `Cross-Origin-Resource-Policy: same-origin` · `Content-Disposition: inline;
  filename="logo.png"` ya da `"logo.jpg"` — **mime'a göre** (tasarım özü sabit `logo.png`
  yazıyordu; JPEG için yanlış uzantı olurdu) · `Cache-Control: private, max-age=31536000,
  immutable` (içerik adresli) · `ETag: "<sha>"`. *(WL-6 düzeltmesi, 2026-10-03: 200 bu
  yediye ek olarak `Content-Length` taşır — gövde chunk'lanmaz (karar 5). `If-None-Match`
  oturumun kendi satırı bulunduktan SONRA değerlendirilir ve eşleşirse 304 döner; 304
  `Cache-Control`, `ETag`, `X-Content-Type-Options`, `Content-Security-Policy`,
  `Cross-Origin-Resource-Policy` taşır, `Content-Type`/`Content-Disposition`/
  `Content-Length`/gövde taşımaz (karar 6). Rotalar yalnız GET'tir: ölçülen yedi standart
  metot — HEAD, POST, PUT, DELETE, PATCH, OPTIONS, TRACE — yönlendiricinin 405'ini
  `Allow: GET` ile, yönlendiricinin tanımadığı bir metot (ölçülen: `FOO`) 405'i `Allow`
  başlığı olmadan alır; ikisi de digest'ten bağımsız (karar 7).)*
- **Sayfa CSP'si:** `img-src 'self'` yalnız `<img>` render eden yanıtın politikasına
  (`tapCSPFor(hasLogo)`, `adminCSPFor(hasLogo)`; emsal `landingCSPFor`, `marketing.go:500-509`).
  `Tap.render` tap, sonuç ve tap-problem yanıtlarında ortaktır (`tap.go:635-646`), politika
  render başına hesaplanır (→ **WL-6**: "sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır"
  testi, panel + tap).
- **Bütçe:** tap grubundaki logo isteği tap sınırlayıcısının bütçesine girer; soğuk önbellekte
  sayfa başına +1 (→ **WL-6**: ücretli istek sayısı ölçülüp bütçeler güncellenir — sıcak 1,
  soğuk 2).
- **Elenen:** kimliksiz hash rotası — tenant'lar arası okuma için `SECURITY DEFINER` gerekirdi
  (ADR 0002 md.7'ye yeni istisna) ve bütçesiz bir DB okuması olurdu.

### 6. Yükleme akışı, yetki, audit, log

- **Yetki:** owner (`mayEditAccount`, `accountactions.go:95-97`); üç `ProtectWriting` rotası —
  logo, accent, sıfırla; aynı-origin kapısı çözümleyiciden önce (→ **WL-7**: cross-origin POST
  çözümleyiciden önce red; manager POST'u 303 `not-permitted` + `tenant.brand_update_refused` +
  0 UPDATE). Askı: `suspensionGate` bu rotaları kapsar (OP-15).
- **Gövde yalnız `r.MultipartReader()` ile okunur.** Logo handler'ı ve onun çağırdığı handler
  paketi fonksiyonları `r.FormValue`, `r.PostFormValue`, `r.FormFile`, `r.ParseMultipartForm`,
  `r.ParseForm` çağırmaz ve `r.Form`, `r.PostForm`, `r.MultipartForm` alanlarını okumaz.
  Gerekçe — ölçülen kapsamıyla: (i) bu okuyucular gövdenin tamamını belleğe alır, parça adını
  ve sayısını sormadan kabul eder; *"tam olarak bir parça, adı `logo`"* kuralı ve
  `LimitReader` akışı `MultipartReader` ister; (ii) `ParseMultipartForm`'u eşiği parçadan küçük
  çağırmak geçici dosya açmaya kalkar ve salt-okunur kökte düşer (S17, S20); (iii) 1 MiB gövde
  tavanı altında `FormFile`/`FormValue`/`PostFormValue` 32 MiB eşiğinin altında kalıp diske
  dökmez (S20) — yani *"`TMPDIR` yokken başarılı"* testi onları `MultipartReader`'dan
  **ayırmaz**; ayıran şey aşağıdaki sözdizimi pinidir. `ProtectWriting` zinciri bugün gövde
  okumaz (Bağlam) (→ **WL-7** kabulü: logo handler dosya(lar)ının AST'sini okuyan bir test
  yukarıdaki beş çağrıyı ve üç alan okumasını arar — mutasyon: handler'a `r.FormValue("x")` →
  kırmızı; ayrıca `TMPDIR` salt-okunur/yokken yükleme başarılı, ikinci ya da bilinmeyen parça
  red).
- **SSS cümlesi:** `FactNoBulkImport`'un türetimi *"marka logosu handler'ı dışında multipart
  okuyucu yok"* olarak yeniden yazılır; SSS cümlesi değişmez. Tel bugün üç yazımı arar
  (`FormFile`, `MultipartReader`, `ParseMultipartForm` — `marketing_facts_test.go:48`);
  `FormValue`/`PostFormValue`'yu aramaz ve logo handler'ını muaf tutar (→ **WL-7**: mutasyon —
  `employeeactions.go`'ya `FormFile` → kırmızı).
- **Yükleme bütçeleri ve okuma süresi** (→ **WL-7**, N ile birlikte **WL-3**). Bugünkü bütçeler
  yüklemeye göre biçilmedi: panel oturumu 10 dakikada 300 istek atabilir (Bağlam), yani tek
  oturum semafor yuvalarını sürekli dolu tutabilir; her logo güncellemesi ≈256 KiB TOAST + WAL
  yazar (oturum başına 10 dakikada ≈75 MiB). Gövde okuma fazı semaforun **önünde** ve süresizdir
  (`ReadTimeout` yok); bugün onu sınırlayan ölçülmüş ayar ingress'in `proxy-body-size: "1m"`'idir
  (istek tamponlaması kümede ölçülmedi). Kurallar:
  - **tenant başına yükleme bütçesi** (pencere başına sayı ve bayt); *(WL-3 düzeltmesi,
    güvenlik denetimi: bütçe **deneme başına** düşülür — gövde okunmadan ve `Normalize`
    çağrılmadan **önce**; sonuca göre düşülen bir bütçede decode'a ulaşan her ret bedava bir
    tam decode olur — `ErrLogoCorrupt`, `ErrLogoOutputTooLarge`, `ErrLogoVerify` ve yuva
    alındıktan sonra biten bağlam. Decode'dan önce dönen retler (`ErrLogoBusy`,
    `ErrLogoInputTooLarge`, `ErrLogoRead`, `ErrLogoFormat`, `ErrLogoDimensions`,
    `ErrLogoScans`, okuma sırasında biten bağlam) decode etmez — tahsisi ölçülen dördünde
    (meşgul, boyut, tarama, okuma sırasında biten bağlam) 4 696–550 400 B (WL-3 notu). Kapı
    süreç genelinde tek yuva, bütçe tenant başına ve
    kayıt herkese açık olduğu için tenant'lar arası açlık sayılı sınır 11'dedir.)*
  - gövde okunmadan önce **eşzamanlı yükleme kabul sınırı** (dolu ise gövde okunmadan ret);
  - gövde okuması için `http.NewResponseController(w).SetReadDeadline`;
  - ingress bağımlılığı adıyla yazılır: `40-ingress.yaml:127-133`'ün *"small JSON body"*
    önkabulü güncellenir ve istek tamponlamasının kümede açık olduğu ölçülür.
  Kabul: bütçe aşımı 429 + `tenant.brand_update_refused` + 0 UPDATE; kabul sınırı doluyken
  ikinci eşzamanlı yükleme gövdesi okunmadan reddedilir; yavaş gönderilen gövde okuma süresinde
  kesilir.
- **Audit:** `tenant.brand_updated`, değişiklikle **aynı transaction'da** `RecordTx`; `detail`
  tam olarak altı sabit anahtar: `field`, `before`, `after`, `bytes`, `width`, `height` — logo
  için `before`/`after` sha256'dır; görsel baytı, dosya adı, istemci `Content-Type`'ı detail'de
  yok. Reddedilen deneme `tenant.brand_update_refused` (→ **WL-4**: detail tam 6 sabit anahtar;
  `ActorID` zorunlu).
- **Log:** ret sınıfıyla loglanır (biçim / boyut / tarama / bütçe); dosya adı ve bayt loglanmaz
  (→ **WL-7**: ret yollarının log satırlarını yakalayan test, dosya adı ve bayt dizisi 0; ayrıca
  **WL-10** denetimi bir kez okur).

### 7. §4.1 ve §4.6

- **§4.1:** logo bir kişinin fotoğrafı olabilir; ürün görsel üzerinde **biyometrik işleme
  yapmaz** ve yüz tespiti **eklenmez** — tespitin kendisi biyometrik işleme olurdu. Form
  metni: *"a logo, not a photo of a person"* (→ **WL-7**).
- **§4.6:** marka ya da logo okuma hatası sayfayı düşürmez; sayfa varsayılana döner ve hata
  loglanır (`PendingBadge` emsali, `review.go:527-555`) (→ **WL-8**, **WL-9**: okuma hatasında
  200 + varsayılan).

## Güvenlik iddiaları — üç parçalı

PART I'deki "ölçülecek" davranışların testleri henüz yoktur; adları kendi görevlerinde konur,
burada **tarifleriyle** yazılır. Sonda ölçümleri (S1–S20) go1.27.1'de alındı.

**İddia A — kabul edilen bayt PNG ya da JPEG olarak decode edilebilir; koklama tek başına karar
vermez.**
- **PART I:** S1, S2 (bugün ölçüldü). WL-3'te ölçülecek: SVG, GIF, WebP, HTML ve imzası PNG olan
  HTML reddedilir; istemcinin `Content-Type`'ı ve dosya adı sonucu değiştirmez.
- **PART II:** WL-3'in biçim tablosu testi (bu beş vaka) · WL-3'in "istemci başlığı yok
  sayılır" testi. Yakaladıkları: tablodaki beş vakadan birinin (SVG, GIF, WebP, HTML, PNG imzalı
  HTML) kabul edilmesi; istemcinin `Content-Type`'ının ya da dosya adının sonucu değiştirmesi
  *(WL-3 düzeltmesi: testin ölçtüğü üç etiketli parçada — WL-3 notu, İddia A)*.
  Kapıların **sırası** (`DecodeConfig`'in `Decode`'dan önce gelmesi) bu testin değil, İddia B'nin
  bomba testinin konusudur: ikinci kapı kaldırılsa da PNG imzalı HTML `Decode`'da ve sıfır
  kenarlı başlık boyut kapısında düşer, bu beş vakalık test yeşil kalır.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia B — boyut ve tarama sınırları decode'dan önce uygulanır.**
- **PART I:** S3–S6, S9, S16 (bugün ölçüldü). WL-3'te ölçülecek: 30000×30000 başlıklı dosya
  decode'dan önce reddedilir ve ayrılan bayt S3/S5 ölçeğinde kalır; tavanın bir fazlası
  taramalı JPEG ve "dürüst olmayan" JPEG (segmentler arası çöp, uzunluk alanına gizlenmiş SOS)
  decode'dan önce reddedilir; kesik dosya reddedilir; eşzamanlı çözme `-race` altında N'yi
  aşmaz; context'i iptal edilen istekten sonra yuva decode bitene kadar dolu kalır. WL-3'te bir
  kez ölçülecek (pin değil): 512Mi konteynerde N eşzamanlı en kötü decode altında RSS *(WL-3
  düzeltmesi: N = 1 ile bir konteynerde ölçüldü, podda değil — WL-3 notu)*.
- **PART II:** WL-3'in bomba testi (tahsis ölçümüyle) · tarama tavanı testi (dürüst olmayan
  vakayla) · kesik girdi testi · semafor testi (iptal vakasıyla) · fuzz testi. Yakaladıkları:
  `DecodeConfig` kapısından önce `Decode` çağrılması; tarama sayımının kaldırılması ya da
  çözücüden az sayan bir sayıma dönüşmesi *(WL-3 düzeltmesi: dürüst olmayan dosya testinin
  yedi yerleşiminden birinde az sayan bir sayıma — WL-3 notu, İddia B PART II; genel "az sayan
  her sayım" iddiası yok)*; yeniden decode'un sınır dışı bir çıktıyı kabul etmesi; semaforun
  atlanması ya da iptalde erken bırakılması; çözücüde panik.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C — saklanan bayt girdinin meta verisini ve ek yükünü taşımaz.**
- **PART I:** S11–S14 (bugün ölçüldü). WL-3'te ölçülecek: EXIF-GPS'li JPEG'in çıktısında
  `Exif` APP1 yok; IEND sonrası yük çıktıda yok; çıktının bölüm listesi S14'teki kümedir.
- **PART II:** WL-3'in meta veri testleri (JPEG APP1/COM, PNG `eXIf`/`tEXt`/`iCCP`, IEND ve EOI
  sonrası bayt) · çıktı bölüm listesi testi. Yakaladıkları: yüklenen baytın saklanması;
  kodlayıcının meta veri yazan bir yolla değiştirilmesi *(WL-3 düzeltmesi: testlerin
  girdilerindeki segment ve chunk'lardan birini çıktıya taşıyan bir yolla — JPEG'de APP1
  `Exif`, APP2 `ICC_PROFILE`, COM, APP15, EOI sonrası bayt; PNG'de `eXIf`, `tEXt`, `zTXt`,
  `iCCP`, IEND sonrası bayt; WL-3 notu, İddia C)*.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia D — bir tenant'ın logosu başka tenant'ın oturumuna sunulmaz ve varlığı ayırt
edilemez.**
- **PART I:** bugün rota ve tablo yok (ölçüldü). WL-1'de ölçülecek: `WHERE`'siz A bağlamı B'nin
  satırını 0 görür; B'nin `tenant_id`'li INSERT'i WITH CHECK ile reddedilir. WL-6'da: A
  oturumunun B'nin sha'sına aldığı 404 bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu
  404.
- **PART II:** WL-1'in RLS testi · WL-6'nın bayt-aynı 404 testi · oturumsuz istek testi ·
  `TestStaffQueries_CarryAnExplicitTenantPredicate` — `internal/domain/tenant`'ın üretim
  dosyalarının çağırdığı her store sorgusunun metninde `@tenant_id` yüklemi arar; marka
  sorgularını ancak o paketten çağrıldıkları sürece görür (WL-4'ün planlanan yeri
  `internal/domain/tenant/brand.go`). Yakaladıkları: RLS politikasının kapanması; o paketten
  çağrılan bir marka sorgusundan tenant filtresinin düşmesi; iki 404 yolunun ayrışması; oturum
  kapısının atlanması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia E — logo yanıtı tarayıcıda belge olarak koşmaz.**
- **PART I:** WL-6'da ölçülecek: yanıt başlıkları §5'teki listeyle birebir.
- **PART II:** WL-6'nın başlık testi · sayfa `img-src` testi. Yakaladıkları: `nosniff`,
  `sandbox`, CORP ya da sabit `Content-Type`'ın düşmesi; `<img>` render etmeyen sayfaya
  `img-src` eklenmesi.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia F — logo handler'ı gövdeyi `MultipartReader` ile, tek parça olarak okur.**
- **PART I:** S17, S18, S20 (bugün ölçüldü; S20: 1 MiB tavan altında `FormFile`/`FormValue`
  geçici dosya yazmaz, yani `TMPDIR` testi onları ayırt etmez). WL-7'de ölçülecek: `TMPDIR`
  salt-okunur/yokken yükleme başarılı; ikinci ya da bilinmeyen parça red; handler dosya(lar)ında
  §6'nın beş form okuyucu çağrısı ve üç alan okuması 0.
- **PART II:** WL-7'nin sözdizimi pini (logo handler dosya(lar)ının AST'si; aradığı küme:
  `FormValue`, `PostFormValue`, `FormFile`, `ParseMultipartForm`, `ParseForm` çağrıları ve
  `Form`, `PostForm`, `MultipartForm` alan okumaları) · `TMPDIR` testi · parça testi ·
  `FactNoBulkImport`'un yeniden türetilmiş teli (`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed`
  içinde). Yakaladıkları: pinin taradığı dosyalarda o sekiz yazımdan birinin kullanılması;
  `ParseMultipartForm`'un parçadan küçük bir eşikle çağrılması (`TMPDIR` testi); ikinci parçanın
  okunması; logo handler'ı dışındaki üretim Go dosyalarında `FormFile`, `MultipartReader` ya da
  `ParseMultipartForm` yazılması (tel — `FormValue`/`PostFormValue`'yu aramaz).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia G — audit ve log görselin baytını ve dosya adını taşımaz.**
- **PART I:** WL-4'te ölçülecek: `detail` anahtar kümesi tam olarak altı sabit anahtar. WL-7'de
  ölçülecek: ret yollarının log satırlarında dosya adı ve bayt dizisi 0. WL-10'da bir kez
  okunacak (pin değil).
- **PART II:** WL-4'ün detail anahtar testi · aynı-tx testi (audit patlarsa UPDATE geri alınır) ·
  WL-7'nin ret-yolu log testi. Yakaladıkları: detail'e yedinci anahtar girmesi; audit'in tx
  dışına çıkması; testin sürdüğü ret yollarında log'a dosya adı ya da bayt yazılması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia H — yükleme yolu süreç kaynaklarını tenant başına ve süre olarak sınırlar.**
- **PART I:** bugün yükleme yolu yok; bugünkü bütçeler ve ingress tavanı ölçüldü (Bağlam).
  WL-7'de ölçülecek: tenant bütçesi aşımı 429 + ret audit'i + 0 UPDATE; kabul sınırı doluyken
  ikinci eşzamanlı yükleme gövdesi okunmadan red; yavaş gönderilen gövde okuma süresinde kesilir.
- **PART II:** WL-7'nin bütçe testi · kabul sınırı testi · okuma süresi testi. Yakaladıkları:
  bütçe sayacının atlanması; gövdenin kabul sınırından önce okunması; okuma süresinin
  konmaması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## Elenen seçenekler

- **SVG + temizleyici** — stdlib'de yok; bağımlılık olurdu ve temizleyici kendi saldırı yüzeyi.
- **Yüklenen baytı olduğu gibi saklamak** — S11–S13'ün yükleri ve EXIF GPS saklanırdı.
- **`golang.org/x/image`** (WebP çözücü, `draw` ölçekleyici) — yeni bağımlılık; CLAUDE.md §1
  kullanıcıya sormayı ister. Bu ADR onsuz yazıldı: kutu filtresi elle, WebP kapsam dışı.
- **Dosya sistemi ya da obje deposu** — kök salt-okunur, volume yok, kümede obje deposu yok.
- **Kimliksiz, sha adresli tek rota** — §5.
- **`ParseMultipartForm` / `FormFile` / `FormValue`** — S17, S20 ve §6'nın gerekçesi (parça
  denetimi, akış, küçük eşikte disk yolu).

## Sayılı sınırlar ve kabul edilen riskler

1. **Ölçümler go1.27.1'de.** CI 1.26.6. *(WL-3 düzeltmesi: "WL-3 testleri sayıları CI'da
   yeniden üretir" yazıyordu — tutmuyor: CI yalnız `-race` koşar ve sayıları üreten üç ölçüm
   testi `-race` altında atlanır; CI'da WL-3'ün kabul pinleri koşar — WL-3 notu, sayılı sınır 2.)*
2. **Tarama tavanı, GC ayarı ve yükleme bütçelerinin sayıları henüz yok** — WL-3 ve WL-7'nin
   kararı, ölçümle (§2.5, §2.6, §6). N = 1, `GOMAXPROCS` = 2 türetmesinin sonucudur; podda
   ölçülen `GOMAXPROCS` 2'den farklı çıkarsa yeniden hesaplanır (§2.6).
3. **Orientation ve ICC düşer** (§3).
4. **HEIC yolu ölçülmedi** (§1).
5. **Kutu filtresi bir kalite tercihidir**, Lanczos değil; küçültülmüş logo yumuşak görünebilir.
6. **Marka taklidi** — ADR 0023 sınır 1 (ADR 0005 eki WL-12'nin kabulünde).
7. **Ingress'e bağımlılık:** gövde okuma fazının bugünkü sınırı ingress'in `proxy-body-size`'ı
   ve istek tamponlamasıdır; tamponlamanın kümede açık olduğu ölçülmedi (§6, WL-7).
8. **Piksel verisine gömülü yük** yeniden kodlamadan sağ çıkabilir (§1); tarayıcı tarafındaki
   kontrol İddia E'nin başlıklarıdır.
9. **`/static`'in başlıkları** (cache, nosniff) ayrı bir sertleştirme işidir; logo `/static`'ten
   sunulmaz.
10. **Önbellek:** logo değişince eski sha'nın URL'i onu daha önce almış tarayıcılarda bir yıl
    durabilir (`private`, içerik adresli); sunucu eski sha'ya 404 verir.
11. **Tenant'lar arası açlık (WL-3 güvenlik denetimi, 2026-10-03).** Decode kapısı süreç
    genelinde tek yuvadır (N = 1), yükleme bütçesi tenant başınadır (§6) ve kayıt herkese
    açıktır (adres başına saatte 3 tenant, Bağlam): birkaç tenant'ın bütçeleri birlikte tek
    yuvayı sürekli dolu tutabilir; ölçülen en kötü decode ≈1–1,7 s (WL-3 notu). O sürede başka
    tenant'ların yüklemeleri `ErrLogoBusy` alır; yuva bir çekirdeği tutar ve N < `GOMAXPROCS`
    tap yoluna bir çekirdek bırakır (konteynerde `GOMAXPROCS` = 2 ölçüldü). Çare
    WL-7'de: §6'nın gövde öncesi kabul sınırı, deneme başına düşülen bütçe ve öneri olarak
    süreç geneli bir deneme tavanı (pencere başına, tenant'tan bağımsız).

## Karar verilmedi

- ~~JPEG tarama tavanının sayısı ve tavanın süre bütçesi~~ → **WL-3 notu (2026-10-03): tavan
  100**, tavandaki ölçülen en kötü süre ≈1,1 s. (Semafor N karara bağlı değil, kuralın
  sonucu: N = 1 — §2.6.)
- ~~GC: `GOMEMLIMIT` mi, `2 × (N × tepe + taban)` hesabı mı~~ → **WL-3 notu: hesap**
  (tepe ≈99 MiB ölçüldü; `GOMEMLIMIT=400MiB` ve `=128MiB` ile beş taze süreçlik ölçüm, iki en
  kötü dosyada tek sürecin RSS tepesinde bir etki ayırmadı); taban podda ölçülmedi.
- ~~Progressive / 4 bileşenli JPEG'i tamamen reddetme alternatifi~~ → **WL-3 notu: seçilmedi.**
- Tenant yükleme bütçesinin, kabul sınırının ve okuma süresinin sayıları (§6) — WL-7.
- E-postada logo (faz 2, CID, ≤32 KiB varyant) — ADR 0023.

## Sonuçlar

- WL-1, WL-3, WL-4, WL-6, WL-7 bu ADR'yi normatif kaynak alır; WL-10 güvenlik denetimi bu
  ADR'nin sekiz iddiasını (A–H) listesine alır.
- **Plandaki sayılardan sapmalar** (kart düzeltmesinde ve §5'in tasarım özünde de): en kötü
  bellek 16 MiB → ölçülen 96,02 MiB (ret alternatifiyle ~64 MiB); JPEG tarama tavanı kuralı
  (üst sınır sayım) eklendi; semaforun kapsamı, GC payı ve CPU ilişkisi yazıldı; yükleme
  bütçeleri eklendi; `Content-Disposition` dosya adı mime'a göre; "4 MP" 2²² okunur; iki logo
  rotasının gerekçesi düzeltildi.
- Yeni bağımlılık yok: `image/png`, `image/jpeg`, `mime/multipart`, `net/http`,
  `crypto/sha256` — stdlib.

## WL-3 notu (2026-10-03; 2. tur aynı gün)

Kod: `internal/brand/logo.go` (`LogoGate`, `Normalize`, okuma, kapılar, tarama sayımı, yeniden
kodlama), `internal/brand/logo_resize.go` (kutu filtresi). `go.mod`/`go.sum` diff'i boş.
Ölçüm ortamı: go1.27.1, go1.26.6 ve go1.26.7, darwin/amd64, Intel i9-9980HK. CI
(`ci.yml:91`) ve Dockerfile (`GO_IMAGE=golang:1.26.6-bookworm`) **1.26.6** kullanır;
`image/jpeg`, `image/png` ve `compress/flate` 1.26.6 ile 1.26.7 arasında diff ile aynı, paketin
testleri 1.26.6'da `-race` ile de koşuldu. Aynı makinede paralel bir yapıcı koşuyordu, süreler
bu yüzden üç koşunun ortancası ve aralığıyla yazıldı. Fikstürler testte kodla üretilir (ikili
dosya eklenmedi): CMYK/YCCK ve progressive JPEG, interlaced PNG, meta veri segmentleri ve
tarama bombaları elle yazılan baytlardır (`logo_forge_test.go`).

### Kararlar

1. **Tarama tavanı: `LogoJPEGScanCeiling = 100`.** Sayım §2.5'in (a) biçimi: ham baytta her
   `FF DA` çifti (`bytes.Count`); çözücüye aynı baytlar verilir. Üst sınır gerekçesi çözücünün
   okunmasıdır. Dayandığı kod 1.26.6 ile 1.27.1'de metin olarak aynıdır (3. tur, fonksiyon
   fonksiyon karşılaştırıldı): `decode` (1.27.1 `reader.go:525-671`, 1.26.6 `:520-666`),
   `fill`, `readByte`, `readByteStuffedByte`, `unreadByteStuffedByte`, `readFull`, `ignore`,
   `scan.go`'nun `findRST`'i, `huffman.go`'nun `ensureNBits`/`decodeHuffman`'ı. Paketler başka
   yerde farklıdır: 1.27.1 standart dışı alt örneklemeli ("flex") üç bileşenli JPEG'i çözer,
   1.26.6 reddeder (`processSOF`, `makeImg`, `convertToRGB`, `receiveExtend`, `processSOS`'un
   MCU geometrisi, `reconstructBlock`, `reconstructProgressiveImage`); `processSOS`'un farkı
   yalnız MCU sayısının hesabıdır, tampon ve işaret okumasına dokunmaz. Bu döngü bir işareti
   akışın iki bitişik baytı olarak okur — `FF` olmayan baytların
   üstünden tek tek hizalanır, dolgu `FF`'lerini tek tek atlar, üst düzey RST'yi ve başıboş
   `FF 00`'ı uzunluksuz işaret sayar — ve en çok iki bayt (dolgu baytı) geri gider, bir tarama
   başlığı ise en az sekiz bayttır; yani çalıştırdığı her tarama kendi `FF DA` çiftinde başlar.
   Tavanın seçimi, ölçüyle:
   - **alt sınır:** ölçülen gerçek kodlayıcılar (256×256 kaynak, ham `FF DA` sayımı) —
     ImageMagick 7.1.2 `-interlace JPEG`: gri **6**, YCbCr **10**, CMYK **18**; libjpeg-turbo
     3.2.0 `cjpeg -progressive` 10, `-optimize` 10, `jpegtran -progressive` 10. S10'un 18'i
     tekrar üretildi. libjpeg-turbo'nun tarama betiği sınırı 100 (S10).
   - **süre:** tavanda tarama-bağlı en kötü dosyalar ≈0,4–1,1 s; tarama sayısından bağımsız,
     **boyutla** sınırlı en kötü dosya (ardışık JPEG, tarama başına blok başına bir bit, 512 KiB'a
     sığan tarama sayısı) ≈1,2–1,5 s (tablo). Yani 100'de tarama bombası, tavan olmadan da var
     olan boyut-bağlı dosyadan pahalı değildir; tavanı düşürmek en kötü süreyi düşürmez.
   - Sayım bir APPn segmentindeki ya da bir küçük resimdeki `FF DA`'yı da sayar (ölçüldü: APP1
     içinde 101 çift → `ErrLogoScans`); bu yönde hata reddetmektir.
2. **Yuvanın kapsamı: inceleme de yuvanın içinde.** Okuma (en çok 512 KiB + 1) yuvanın
   dışında; sonra yuva beklemeden alınır, sonra §1–§2.5 kapıları, decode, filtre, kodlama ve
   çıktının ikinci decode'u; yuva `Normalize` dönerken ertelenmiş çağrıyla bırakılır. §2.6 yuvayı
   *"decode'dan önce"* der; WL-3 kapıları da içine aldı. Gerekçe, ölçüyle: paletli bir PNG'de
   `png.DecodeConfig` IDAT'a kadar ek chunk'ları ayrıştırır ve her atlanan chunk için 4 KiB'lık
   bir tampon heap'e kaçar — PLTE'den sonra 37 000 boş `tEXt` taşıyan 518 100 baytlık dosyanın
   yalnız incelemesi **144,5 MiB** kısa ömürlü tahsis ve ≈31 ms CPU (heap nesne tepesi 4,2 MiB);
   yuvanın dışındaki inceleme N ile sınırlanmazdı. Pin (2. tur): dolu kapıda aynı dosya
   **537 672 B** ile `ErrLogoBusy` alır; incelemeyi yuvadan önce koşturup hatasını yuvadan sonra
   döndüren mutant (K09, M31) aynı istekte 152,6 MB ve 42 ms ayırdı.
3. **Okuma ile yuva arasında bağlam denetimi (2. tur, güvenlik denetimi #3).** Gövdesi
   okunurken bağlamı biten istek yuvayı almaz: `context.Canceled`, yuvanın içinde aşama 0,
   539 256 B. Önce: aynı istek yuvayı alıp 100 taramalı CMYK dosyayı sonuna kadar decode
   ediyordu (denetçi: yuva 766 ms tutuldu; mutant M32: 100,8 MB).
4. **Okuma: iki tampon (2. tur).** `io.ReadAll`'ın büyüme politikası sürüme göre değişiyor —
   aynı 518 KB yükleme go1.27.1'de 1 065 360 B, go1.26.6 `-race`'te 2 128 048 B ayırdı. Okuma
   artık 4 KiB'lık bir tampon ve, o dolarsa, sınır + 1 baytlık tek bir tamponla yapılır. Okuma
   tahsisinin **üst sınırı sınır + 32 KiB**'tır (`TestLogoRead_AllocatesBoundedByTheLimit`;
   sınır + 1, Go'nun 8 KiB'lık sayfalarında 520 KiB). Ölçülen, go1.27.1 ve go1.26.6, dokuz boyut
   × üç okuyucu biçimi = 27 okuma: ikinci tamponu gerektiren 17'sinde (4 097 bayt ve üstü üç
   biçimde; 4 096 bayt "bütün" ve "bayt bayt" biçimde) 536 624–537 728 B; ilk tamponda biten
   10'unda (0, 1 ve 4 095 bayt üç biçimde; EOF'u son veriyle dönen "yarım" biçimde 4 096 bayt)
   4 144–5 248 B. WL-7 için: eşzamanlı okuma başına en çok ≈0,52 MiB.
5. **GC: `2 × (N × tepe + taban) < 512Mi` hesabı; `GOMEMLIMIT` öneri olarak.** Çağrı başına
   ölçülen tepe (tablo) **≈99 MiB** (progressive 4:4:4 CMYK, 2048²: `TotalAlloc` 98,45 MiB, heap
   nesne tepesi 99,0–99,3 MiB) → N = 1 ile hesap **taban < 157 MiB** ister. Taban — ürün
   sürecinin podda boştaki RSS'i — ölçülmedi (sayılı sınır 1). `GOMEMLIMIT` ölçüldü, beş taze
   süreçlik aralıklarla (üç ardışık çağrı): CMYK'da ayarsız 138,6–154,7 · `GOMEMLIMIT=400MiB`
   138,7–139,0 · `=128MiB` 138,2–154,5 · `GOGC=50` 138,8–154,5 MiB; 16-bit PNG'de 144,8–146,2 ·
   145,1–145,8 · 144,8–145,6 · 105,8–137,4 MiB. CMYK iki kiplidir (≈139 ve ≈154); beş koşu
   `GOMEMLIMIT`'in bir etkisini ayırmaya yetmedi. Tepe tek decode'un canlı patlamasıdır;
   `GOMEMLIMIT` bütün sürecin GC payını sınırın yakınında sıkar. **Öneri (manifest
   değiştirilmedi):** `deploy/k8s/20-app.yaml` uygulama konteynerinin `env`'ine
   `GOMEMLIMIT=400MiB` — ürün geneli bir kemer olarak; logo yolu için gereken, tabanın podda
   ölçülmesidir.
6. **Progressive / 4 bileşenli JPEG'i reddetme alternatifi seçilmedi.** Hesap ≈99 MiB tepeyle
   taban < 157 MiB'ta tutuyor; ret tepeyi ~64 MiB'a indirir ama tasarımcıların CMYK baskı
   logolarını keser. Taban podda ölçülüp 157 MiB'ı aşarsa yeniden açılır.
7. **Kutu filtresi:** alan ağırlıklı ortalama, tamsayı aritmetiği (filtrenin pikselleri
   mimariden bağımsız — karar 10'a bakınız, saklanan bayt değil), premultiplied 16-bit örnekler
   (saydam piksel komşusuna renk vermez), en yakına yuvarlama; büyütmez — sığan logo boyutunu
   korur (`TestLogoOutput_Within512AndTheSizeLimit`, M22). Maliyet: 2048² → 512² **80–132 ms**,
   1,22 MB/çağrı (`BenchmarkLogoResize_2048To512`, beş decode tipi). Kalite: 2048²'lik 1 px
   siyah-beyaz dama tahtası 512²'de 127/128 gri
   (`TestLogoResize_FineDetailAveragesInsteadOfAliasing`); kendi boyutunda her piksel stdlib'in
   NRGBA dönüşümüyle aynı (`TestLogoResize_IdentityWhenItFits`; düşük alfada bir seviye kayıp —
   `{133 8 140 15}` → `{133 7 140 15}` — premultiplied filtrenin bedeli).
8. **256 KiB stratejisi:** §3'ün kendisi — PNG → PNG `BestCompression`, JPEG → JPEG q85, aşan
   → `ErrLogoOutputTooLarge`; kalite merdiveni ya da biçim değişimi eklenmedi (ADR açık
   bırakmıyor). Ölçü: 512² gürültü JPEG q85 çıktısı **198 617 B** (sınırın altında); 400²
   gürültü PNG (480 673 B girdi) → `ErrLogoOutputTooLarge`.
9. **Hata metni sınıfın metnidir.** Ret sınıfları `ErrLogoBusy`, `ErrLogoInputTooLarge`,
   `ErrLogoRead`, `ErrLogoFormat`, `ErrLogoDimensions`, `ErrLogoScans`, `ErrLogoCorrupt`,
   `ErrLogoOutputTooLarge`, `ErrLogoVerify` (§6'nın log sınıfları için WL-7 eşler). Çözücünün
   ve okuyucunun mesajı girdiden değer taşıyabilir (`image/png`: *"Bad chunk length: %d"*,
   *"bit depth %d, color type %d"*; `mime/quotedprintable`: *"invalid unescaped byte 0x%02x in
   body"* — `multipart.Part` bu aktarım kodlamasını çağıranı için çözer); `errors.As` ile
   erişilir, `Error()` metnine girmez (`TestLogoErrors_TextIsTheClassOnly`: iki neden de
   ölçüldü; M18 — `Error()` nedeni taşır — ve M37 — `ErrLogoRead` `fmt.Errorf("%w: %w")` ile
   nedeni taşır — KIRMIZI).
10. **Saklanan baytın sha256'sı Go sürümleri arasında kararlı değildir (2. tur, güvenlik
    denetimi #5; yeniden ölçüldü).** 300×200 gürültü PNG'nin çıktısı go1.26.6'da 180 383 B,
    go1.27.1'de 180 355 B; JPEG çıktısı ikisinde aynı (47 765 B, aynı sha). Filtre değil,
    kodlayıcı (`compress/flate`) değişiyor. Devirler aşağıda.

### Ölçümler

Süreler — `Normalize` uçtan uca (inceleme + decode + filtre + kodlama + ikinci decode),
2048×2048, üç koşunun ortancası (aralık), `TestLogoScans_WorstCaseDecodeTime`:

| Dosya | Tarama · bayt | go1.27.1 | go1.26.7 |
|---|---|---|---|
| gri progressive, DC + 99 ilk AC (EOB koşusu) | 100 · 10 157 | 367 ms (364–374) | 422 ms (389–424) |
| gri progressive, DC + 1 AC + 98 iyileştirme | 100 · 10 157 | 911 ms (907–933) | 955 ms (741–956) |
| CMYK 4:4:4 progressive, DC + 99 ilk AC | 100 · 34 755 | 525 ms (522–550) | 574 ms (520–590) |
| CMYK 4:4:4 progressive, DC + 4 AC + 95 iyileştirme | 100 · 34 755 | 1 052 ms (1 049–1 058) | 1 068 ms (870–1 131) |
| gri progressive, 60 DC + 1 AC + 39 iyileştirme | 100 · 493 249 | 700 ms (542–723) | — |
| CMYK progressive, 15 DC + 4 AC + 81 iyileştirme | 100 · 493 661 | 1 113 ms (831–1 297) | — |
| gri ardışık, 60 tarama (boyut-bağlı) | 60 · 492 949 | 1 327 ms (1 240–1 366) | 1 153 ms (1 062–1 228) |
| CMYK 4:4:4 ardışık, 15 tarama (boyut-bağlı) | 15 · 492 536 | 1 480 ms (1 464–1 662) | 1 293 ms (1 218–1 335) |

Tek koşuda gözlenen en uzun: 1 662 ms. `-race` altında aynı 2048² iyileştirme dosyası ≈9 s
sürdü (bir kez gözlendi). Bütçe: router'ın 30 s'si; testin tek iddiası bu. (Süreler 1. turun
kodunda ölçüldü; 2. turun değişiklikleri — okuma, bağlam denetimi — decode yoluna girmez.)

Bellek, yerel süreç — `TestLogoMemory_WorstDecodeAllocations` (çağrı başına `TotalAlloc`,
100 µs'de bir örneklenen heap nesne tepesi; go1.27.1, parantez go1.26.6) ve
`TestLogoMemory_ProcessRSS` / `TestLogoMemory_Child` (her ölçüm taze bir alt süreç, üç ardışık
çağrı, `GOGC`/`GOMEMLIMIT` ayarsız; darwin'de `getrusage` tepe RSS, sürecin decode öncesi RSS'i
6,1–6,7 MiB; beş süreçlik aralık, go1.27.1):

| Dosya (2048²) | TotalAlloc | heap tepe | süreç RSS tepe |
|---|---|---|---|
| progressive CMYK 4:4:4 JPEG | 98,45 (98,45) MiB | 99,30 (99,03) MiB | **138,6–154,7 MiB** |
| 16-bit RGBA interlaced PNG | 68,22 (67,91) MiB | 65,59 (65,28) MiB | 144,8–146,2 MiB |
| 8-bit RGBA PNG | 20,05 (19,74) MiB | 20,92 (20,29) MiB | 44,1–59,3 MiB |
| 4:2:0 JPEG | 8,29 (8,30) MiB | 9,19 (8,87) MiB | 22,1–28,5 MiB |

RSS'te en kötü dosya CMYK'dır (iki kipli: ≈139 ve ≈154 MiB); 1. turda tek değer (137,4) yazılmıştı.

**Konteynerde ölçüldü (pod değil, ürünün kendi tabanı dahil değil) — WL-3'ün üçüncü gözü,
2026-10-03:** go1.26.6 linux/amd64, `docker --memory=512m --cpus=2`. `GOMAXPROCS` = **2** (NumCPU
4, `cpu.max` "200000 100000") → §2.6'nın N = 1 türetmesi tutuyor. Taze süreç, üç ardışık çağrı,
`VmHWM` / cgroup `memory.peak`: CMYK progressive 2048² **137–153 / 139–155 MiB**; 16-bit
interlaced PNG 113–126; 8-bit RGBA PNG 49–58; 4:2:0 JPEG 21–23 MiB. Kalan: podda ürün tabanıyla
RSS — deploy'da orkestratörün (WL-7'ye bloke olarak).

Ret yolları (2. turun kodu, go1.27.1) — `TestLogoBomb_HugeHeaderRefusedBeforeDecode`: 30000×30000
PNG (75 B; go1.26.6/7'de 72 B — `compress/flate` çıktısı sürüme göre değişiyor) çağrı boyunca
**5 392 B**, 30000×30000 JPEG (219 B) **17 920 B** ayırır (4 KiB'lık okuma tamponu dahil);
kontrol: kapıdan geçen 2048² başlıklı verisiz PNG decode edilir, 16,8 MB.
`TestLogoScans_CeilingAcceptedOneMoreRefused`: 101 taramalı 2048² gri ve CMYK ret 550 400 B
(aynı yerleşimin decode'u 21,0 MB / 100,7 MB). `TestLogoGate_FullGateRefusesBeforeDecoding`:
dolu kapıda 2048² progressive CMYK 537 768 B, 37 000 `tEXt`'li paletli PNG 537 672 B, SVG
4 696 B — üçü de `ErrLogoBusy`; yuva boşken ilk ikisi 102,4 MB ve 305,5 MB.
`TestLogoGate_ContextEndedDuringTheReadTakesNoSlot`: 539 256 B, yuva 0 kez.
`FuzzNormalize`: 1. turda 60 s / 3 584 572 ve 45 s / 1 875 674 çalıştırma, 2. turun kodunda
kısa koşu (teslim raporunda), hata 0; korpus tohumları her `go test`'te koşar.

### Güvenlik iddiaları — WL-3'ün ölçtüğü parçalar (üç parçalı)

**İddia A (WL-3).**
- **PART I:** `TestLogoFormat_RefusesWhatIsNotPNGOrJPEG` — listelediği on bir girdi (SVG, XML
  önsözlü SVG, GIF, WebP, HTML, PNG imzalı HTML, JPEG imzalı HTML, BMP, PDF, düz metin, boş)
  ve SOI'den sonra bir çöp baytı taşıyan JPEG (çözücü okur, koklama JPEG demez) `ErrLogoFormat`
  alır; aynı resmin PNG'si ve JPEG'i kendi biçiminde normalize olur.
  `TestLogoFormat_ClientHeadersDoNotDecide` — üç etiketli multipart parçası: `image/png` +
  `logo.png` etiketli SVG `ErrLogoFormat`; `image/jpeg` + `logo.jpg` etiketli, SOI'den sonra
  çöp baytlı JPEG `ErrLogoFormat` (etiket koklamanın yerine geçmiyor); `image/svg+xml` +
  `logo.svg` etiketli PNG, etiketsiz okuyucununkiyle bayt-aynı çıktıyla kabul.
  `TestLogoDecode_CallsTheSniffedFormatsOwnDecoder` — `logo.go` ve `logo_resize.go`'da `image`
  paketinin `Decode`/`DecodeConfig` çağrısı 0, biçimin kendi dört çağrısı mevcut.
- **PART II:** biçim tablosu testi — yakaladığı: listedeki on iki ret girdisinden birinin kabul
  edilmesi; koklama kapısının çözücüleri denemeye çevrilmesi (mutasyon M17). Başlık testi —
  yakaladığı: üç girdisinde parçanın `Content-Type`'ının ya da dosya adının sonucu
  değiştirmesi; `image/png`/`image/jpeg` etiketinin çözücüyü seçmesi (M34 — denetçinin K10b'si).
  Sözdizimi pini — yakaladığı: iki dosyada `image` içe aktarmasının yerel adıyla yazılmış bir
  `Decode`/`DecodeConfig` çağrısı (M27).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia B (WL-3).**
- **PART I:** bomba testi (yukarıdaki tahsisler); `TestLogoDimensions_EachEdgeAndThePixelCount`
  — on bir boyut × iki biçim, sıfır ve negatif genişlikli PNG (`ErrLogoFormat`), yüksekliği 0
  olan JPEG (`ErrLogoDimensions`); tavan testi — 100 tarama kabul, 101 `ErrLogoScans`;
  `TestLogoScans_DishonestFilesRefused` — **yedi yerleşim**, her biri kandırdığı sayaçla
  adlandırılmış: DC taramasından önce üst düzey `FF D0`, `FF 00`, dolgu `FF FF`, `00 11 22`
  (segment uzunluğunu izleyen yürüyücü); yükü `FF DA FF FF` olan bir APP1 (her bulduğu `FF DA`'nın
  "başlığını" ardındaki iki baytla atlayan sayaç — denetçinin K01'i); DQT değerlerinde
  `FF E1 FF F0` (`FF Ex` gördüğü yerde APPn yükünü uzunlukla atlayan sayaç — K02); yükü `FF D9`
  olan bir APP1 (ilk `FF D9`'da duran sayaç — K04; gerçek dosyalarda EXIF küçük resminin EOI'si
  orada durur). Her birinde, çözücünün çalıştırdığı 101 tarama için adlandırılan sayaç ≤ 100
  sayar, stdlib çözücü dosyayı çözer ve her piksel DC taramasının seviyesindedir (77), ham sayım
  > 100 → `ErrLogoScans`; ham sayımı tam 100 olan aynı yerleşim kabul. Kontrol: dürüst dosyada
  dört sayaç ham sayımı verir.
  `TestLogoTruncated_EveryPrefixRefused` — dört dosyanın (PNG, JPEG, progressive JPEG,
  interlaced PNG) her öneki ret. `TestLogoInput_OverTheLimitRefusedBeforeInspection`,
  `TestLogoRead_AllocatesBoundedByTheLimit` (dokuz boyut × üç okuyucu biçimi: dönen bayt
  yüklemenin öneki, tahsis ≤ sınır + 32 KiB). Semafor: `TestLogoGate_ConcurrentDecodesNeverExceedN`
  (`-race`, N = 1 ve 3, 16 goroutine × 5: içerideki çağrı sayısının tepesi N),
  `TestLogoGate_FullGateRefusesBeforeDecoding` (dolu kapıda üç yükleme ≤ 1 MiB ile `ErrLogoBusy`,
  yuvanın içinde aşama 0), `TestLogoGate_ContextEndedDuringTheReadTakesNoSlot`,
  `TestLogoGate_SlotHeldThroughDecodeResizeEncode` (beş aşamanın her birinde dışarıdan gelen
  ikinci yükleme `ErrLogoBusy`), `TestLogoGate_CancelledContextKeepsTheSlotUntilTheDecodeReturns`
  (iptalden sonra 200 ms boyunca ve canlı 1024² decode süresince her yoklama `ErrLogoBusy`; çağrı
  `context.Canceled` döner, sonra yuva boş), `TestLogoGate_ErrorPathsGiveTheSlotBack`;
  `TestLogoVerifyOutput_RefusesWhatTheFilterDidNotMake`; `FuzzNormalize`. Bir kez ölçülen (pin
  değil): yukarıdaki süre ve bellek tabloları.
- **PART II:** yakaladıkları, mutasyonla: `Decode`'un `DecodeConfig`'ten önce çağrılması (M01,
  bomba testi); kenar sınırının 2049'a (M02) ya da alt kenarın 15'e (M04) gevşemesi (boyut
  testi); sayımın kaldırılması (M06), bir fazla tavan (M07), tarama kapısının düşmesi (M08)
  (tavan testi); sayımın, dürüst olmayan dosya testinin yedi yerleşiminden birinde az sayan
  adlandırılmış dört sayaçtan birine dönmesi — segment yürüyücüsü (M05), başlığı uzunlukla
  atlayan (M29, K01), APPn'i uzunlukla atlayan (M30, K02), ilk `FF D9`'da duran (M36, K04);
  yuvanın iptalde (M12) ya da `Decode` dönünce
  (M13) bırakılması, beklemeli alma (M14), semaforun kaldırılması (M15), decode sonrası iptal
  denetiminin kalkması (M20), incelemenin yuvadan önce koşması (M28: hatası önce; M31, K09:
  hatası yuvadan sonra — dolu kapıdaki chunk fırtınasının tahsisi), okuma ile yuva arasındaki
  bağlam denetiminin kalkması (M32) (semafor testleri); okumanın sınırı +1'siz tutması (M21) ve
  `io.ReadAll`'a dönmesi (M35, dolu kapı testinin 1 MiB'ı — go1.27.1'de 1 065 360 B); çıktının
  ikinci denetiminin atlanması (M19); fuzz korpusunda panik ya da sınır dışı başarı.
  Yakalamadığı, sayılı: piksel sınırının 2²³'e gevşemesi (M03) — eşdeğer mutant: iki kenar
  ≤ 2048 iken çarpım ≤ 2²² (aritmetik), piksel sınırı bağlamaz.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C (WL-3).**
- **PART I:** `TestLogoMetadata_PNGChunksAndTrailingBytesDoNotSurvive` — `eXIf` (GPS IFD'li
  TIFF), betikli `tEXt`/`zTXt`, `iCCP` ve IEND sonrası HTML taşıyan PNG'nin çıktısı tam olarak
  `IHDR IDAT IEND`, IEND'den sonra 0 bayt, yedi iğne (`<script`, `<html`, TIFF imzası, dört chunk
  adı) 0. `TestLogoMetadata_ExifGPSDoesNotReachTheOutput` — GPS IFD'li `Exif` APP1, APP2
  `ICC_PROFILE`, betikli COM, HTML'li APP15 ve EOI sonrası HTML taşıyan JPEG'in çıktısı son
  baytında EOI'ye temiz yürür, işaretleri S14 kümesinde (`SOI DQT SOF0 DHT SOS EOI`), `Exif`, iki
  TIFF imzası, `ICC_PROFILE`, `<script`, `<html` ve `FF E1` 0. `TestLogoOutput_JPEGQualityIs85` —
  çıktının DQT'si `image/jpeg`'in q85 tabloları.
- **PART II:** yakaladıkları: yüklenen baytın saklanması (M09), IEND sonrası baytın korunması
  (M10), girdinin APP1'inin (M11) ya da APP2'sinin (M33, denetçinin K05'i) çıktıya taşınması,
  kalitenin değişmesi (M25). Testlerin girdilerinde olmayan bir segment ya da chunk'ın çıktıya
  taşınması bu listede değildir.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

### Devirler (2. tur, güvenlik denetimi)

- **WL-1 / WL-6 — `logo_sha256`:** sütunun üzerine **global UNIQUE konmaz**: iki tenant aynı
  baytı yüklediğinde ikincinin yazımı hata verir ve başka bir tenant'ta aynı logonun varlığını
  sızdırır (İddia D'nin kehaneti). Ya `(tenant_id, logo_sha256)` ya hiç. "Logo değişmedi mi"
  denetimi yüklemeyi yeniden normalize edip sha karşılaştırmaz — Go sürümü değişince aynı girdi
  başka bayt verir (karar 10).
- **WL-7:** (i) `NewLogoGate` N ≥ 1'in her değerini kabul eder; kapının wiring'de bir kez
  `LogoDecodeSlots` ile kurulduğunu pinleyen test WL-7'nin. (ii) Açık logo uyarısı (ADR 0023)
  `Normalize`'ın çıktısından hesaplanır; yüklenen bayt kapının dışında yeniden decode edilmez.
  (iii) Gövde okuması yuvanın dışındadır: §6'nın kabul sınırı gelene kadar eşzamanlı okuma
  başına ≈0,52 MiB (karar 4). (iv) `ErrLogoBusy` sunucu tarafında yeniden denenmez; kullanıcıya
  "tekrar dene" döner. (v) Yükleme bütçesi deneme başına ve gövde okunmadan / `Normalize`'dan
  önce düşülür (§6 düzeltmesi); tenant'lar arası açlık sayılı sınır 11.

### Sayılı sınırlar (WL-3)

1. **Podda ölçülmedi; konteynerde ölçüldü.** 512Mi/2 CPU'lu bir konteynerde `GOMAXPROCS` ve
   üç ardışık çağrının tepe RSS'i WL-3'ün üçüncü gözünce ölçüldü (yukarıda); ürünün kendi
   tabanıyla podda RSS ölçülmedi — deploy'da orkestratörün. Taban bu yüzden bilinmiyor.
2. **Ölçüm testleri `-race` altında atlanır** (`TestLogoScans_WorstCaseDecodeTime`,
   `TestLogoMemory_WorstDecodeAllocations`, `TestLogoMemory_ProcessRSS`): CI yalnız
   `go test -race -count=1 ./...` koşar (`Makefile:248`, `ci.yml:200-201`), yani bu üçü CI'da
   koşmaz ve S7–S9'un sayılarını CI yeniden üretmez; sayılar `-race`'siz yerel koşudandır.
   Kabul pinleri `-race` altında koşar.
3. **Süreler bu makinede ve yük altında**; başka donanımda farklıdır.
4. **Üst sınır kanıtı çözücünün okunmasına dayanır**: karar 1'in adlandırdığı fonksiyonlar
   1.26.6 ve 1.27.1'de aynı metindir; `image/jpeg` paketinin bütünü değil (1.27.1'in "flex"
   alt örneklemesi). Go'nun `image/jpeg`'i değişirse o fonksiyonlar yeniden okunur. Dürüst
   olmayan dosya testi yedi yerleşimi ve adlandırdığı dört sayacı ölçer. Sürümler arası kabul
   farkı (3. tur, ölçüldü): Y 2×2, Cb 1×1, Cr 2×1 alt örneklemeli 64×64 bir JPEG go1.27.1'de
   normalize olur, CI'nin go1.26.6'sında `DecodeConfig`'te düşer ve `ErrLogoFormat` alır.
5. **PNG chunk fırtınası:** 37 000 boş `tEXt` (518 KB) RGBA PNG'de çağrı 146,8 MiB kısa ömürlü
   tahsis ve ≈34 ms; paletlide 291,4 MiB ve ≈73 ms (heap nesne tepesi 4,2–4,5 MiB) — yuvanın
   içinde (dolu kapıda 537 672 B, karar 2), chunk sayısı kapısı eklenmedi.
6. **Düşük alfada bir seviye kayıp** (karar 7) ve §3'ün Orientation/ICC bedelleri.
7. **Fuzz:** yerel koşular, hata 0; sürekli fuzz koşusu kurulmadı.
8. **Kaçış denemeleri** (bir kez, yukarıdakilerin dışında): 100 MiB'a açılan zlib (205 KB) →
   `ErrLogoCorrupt`, 0,49 MiB; sayıları aşan DHT, `Pq=2` DQT, APP14'süz 4 bileşen, ikinci SOF
   30000² → `ErrLogoCorrupt`; SOF9, SOF3, 12-bit SOF1, 16-bit paletli PNG, GIF başlığı + PNG,
   BOM + PNG, boşluk + PNG → `ErrLogoFormat`; IDAT CRC'si bozuk → `ErrLogoCorrupt`; APNG
   (`acTL`/`fcTL`/`fdAT`) → durağan PNG, çıktı `IHDR IDAT IEND`; arka arkaya iki PNG → ilki;
   ≈512 KiB DHT segmenti → 5 ms, kabul.
9. **Linux'ta RSS ölçümü `VmHWM`'yi okur** (`ru_maxrss` exec boyunca ebeveynden miras kalır —
   denetçinin konteynerinde önce = sonra = 141,9 MiB ölçüldü); test, decode öncesi tepe 32 MiB'ı
   aşan ya da sonrakinden küçük olmayan bir okumayı reddeder. Linux dalı bu makinede derlendi
   (`GOOS=linux`); kapanış denetçisi onu `docker --rm --memory=512m --cpus=2` golang:1.26
   (go1.26.8, CI'nin 1.26.6'sı değil) ile koşturdu: decode öncesi 8,0–13,2 MiB; CMYK progressive
   2048² 139,4–145,8, 16-bit interlaced PNG 119,8–126,4, 8-bit RGBA PNG 45,2–61,3, 4:2:0 JPEG
   23,3–23,7 MiB; `-race -cover` geçti (%94,4).
10. **Saklanan baytın sha256'sı Go sürümüne bağlıdır** (karar 10).

## WL-6 notu (2026-10-03; 2. tur aynı gün)

Kod: `internal/handler/brandlogo.go` (iki rota — `BrandLogos`; sayfa politikasının img-src
yarısı — `logoImagePolicy`; `If-None-Match` taraması; tek ret yazıcısı) ·
`internal/domain/tenant/brandread.go` (`BrandReader.Logo`, `BrandReader.PageLogo`,
`logoRefOf`) · `internal/handler/tap.go` (`Tap.chain`, `tapCSPFor`; `Tap.Mount` zinciri
`Tap.chain`'den alır) · `internal/handler/adminlogin.go` (`adminCSPFor`; `render` ve
`renderScripted` politikayı bu iki fonksiyondan alır) · `cmd/tappa/main.go` (kablo). Bütçe
aritmetiği: `internal/httpx/ratelimit.go` (yorum) ve `ratelimit_test.go`,
`internal/handler/adminratelimit.go` (yorum). Testler: `internal/handler/brandlogo_test.go`,
`brandlogo_db_test.go`, `internal/domain/tenant/brandread_db_test.go`. Yeni sorgu, migration,
bağımlılık yok (`db/`, `internal/store/`, `go.mod`, `go.sum`, `sqlc.yaml` diff'i boş); hiçbir
şablona dokunulmadı (`web/` diff'i boş — bugünkü sayfaların HTML'i değişmedi). Ölçüm ortamı:
dev Postgres 17 (paylaşılan), `tappa_app`; yerel go1.27.1 darwin/amd64, staticcheck
go1.26.7; ayrı git worktree. 1. tur `377daf3` tabanında; 2. tur (üçüncü göz ve güvenlik
denetimi ONAY sonrası bulgular) `f3c9c04` tabanında — OP-10B'nin `NewAdminAuth`'tan yasal
metin parametresini ve `PanelSections`'tan `/admin/legal`'ı çıkardığı ağaç. Bu turun ADR
değişikliği §5'e satır içi *"WL-6 düzeltmesi"* notlarıdır (kural metni değişti: 404'ün
başlıkları, `Content-Length`, 304, yalnız GET).

### Kararlar

1. **Zinciri rota değil yüzey seçer.** `NewBrandLogos(reader, *AdminAuth, *Tap, log)`
   middleware değil iki handler'ı alır: panel rotası `AdminAuth.Protect()`'in arkasında
   (flood → requireAdmin → sessionGate — okuma zinciri), tap rotası `Tap.chain()`'in
   arkasında (ByAddress → Identify → BySession). `Tap.Mount` sırayı aynı fonksiyondan alır:
   sıra tek yerde yazılı ve logo isteği sayfa ile düğmenin **aynı** `TapLimiter` örneğinden
   düşer.
2. **Tap rotası yalnız `SessionLive`'a hizmet eder.** Çerez yok, oturum adlandırmayan çerez,
   iptal edilmiş oturum, yalnız panel çerezi → tek 404. `SessionUnresolved` (zincir kurulmamış
   ya da çözümleme düştü — `BySession` ikincisine zaten 500 verir) → 500 + ERROR log, "oturum
   yok" diye okunmaz (tap.go'nun kuralı). Devre dışı bırakılmış çalışanın oturumu canlıdır
   (M5-01: deaktivasyon oturumu iptal etmez) ve logoyu alır — `GET /t` aynı oturuma sayfayı
   render ettiği için (`TestTapPage_LiveSessionOfADeactivatedEmployeeStillRenders`).
3. **Panel rotası rol ayırmaz** (owner ve manager): WL-8'in kabuğu logoyu iki role de
   gösterir. Panel oturumu olmayan istek `Protect`'in oturumsuz yanıtını alır (303 →
   `/admin/login`); çalışan çerezi panel zincirinde okunmaz.
4. **Tek ret (404):** gövde `Not found.\n`; başlıklar `Content-Type: text/plain;
   charset=utf-8`, `Content-Length`, `Cache-Control: no-store`, `X-Content-Type-Options:
   nosniff`. Rotanın eşleştiği yollarda başka işletmenin digest'i, kimsenin saklamadığı
   digest, `^[0-9a-f]{64}$` dışı `{sha}` segmenti (logo okumasına gitmeden — oturum
   çözümlemesi zincirde ondan önce olmuştur) ve tap rotasında canlı oturumsuz istek bunu
   alır; rotayla eşleşmeyen yol şekilleri (boş segment, segmentte ham `/`) yönlendiricinin
   kendi 404'ünü alır (sınır 3). `http.NotFound` değil: aynı URL logoyu tutan oturuma 200
   verir; önbellekte kalan bir 404 o oturumun logosunu gölgelerdi — `no-store` bu yüzden.
5. **Başlıklar:** §5'in yedisi + `Content-Length` (256 KiB'a kadar gövde chunk'lanmasın;
   recorder ile tel aynı başlık kümesini görsün). `Content-Type` saklanan sütundur: sunum
   anında koklanmaz, istekten okunmaz. Dosya adı bu sütundan: `image/png` → `logo.png`,
   `image/jpeg` → `logo.jpg`; başka bir tür (00028'in CHECK'i varken ulaşılamaz) sunulmaz,
   500.
6. **`If-None-Match` → 304, ve yalnız oturumun kendi satırı bulunduktan SONRA.** "Yok say"
   elendi: ETag gönderen bir origin için koşul yanlışken tam gövdeyi göndermek koşullu isteğin
   sözleşmesine aykırı (RFC 9110 §13.1.2; RFC metni bu görevde ağdan okunmadı). Sıra: önce
   2xx olmayacak yanıtlar, sonra önkoşul — Go'nun kendi `serveContent`'i de böyle yapar
   (go1.27.1 `net/http/fs.go`, `checkPreconditions` dosya bulunduktan sonra; okundu). Bu sıra
   kehaneti kendiliğinden kapatır: başlık yalnız oturumun kendi logosunda yanıtı değiştirir;
   başka işletmenin ya da bilinmeyen digest'te 404 başlıktan bağımsızdır. Karşılaştırma zayıf
   (`W/` atılır), `*` eşleşir. Bozuk bir üye taramayı bitirir (net/http'nin `scanETag`
   taraması): ondan ÖNCE eşleşme yoksa 200 (`garbage, "<sha>"`, `"x` + `"<sha>"`); eşleşme
   ondan önce bulunmuşsa 304 (`"<sha>", garbage` · `*garbage` · `"<sha>" junk` · iki satır
   `"<sha>"` + `garbage`) — ölçüldü (`TestIfNoneMatch_TheScanFollowsRFC9110`); tek satır
   içinde net/http'nin `checkIfNoneMatch`'iyle aynı davranış. **Satırlarda farklı (3. tur):**
   net/http yalnız ilk `If-None-Match` satırını okur, bu rota bütün satırları birleştirir —
   [`"x"`, `"<sha>"`] iki satırında net/http 200, bu rota 304 verir (go1.27.1
   `http.ServeContent` ile scratchpad sondasında ölçüldü; aynı sondada tek satırlık sekiz
   şeklin sekizinde ve iki satırlı üç şeklin ikisinde iki sonuç aynı, farklı olan yalnız bu
   şekil). Fark yalnız oturumun kendi logosunda ortaya çıkar ve
   sonucu 304'tür; başka işletmenin ya da bilinmeyen digest'in 404'ü bu değerlendirmeden önce
   verilmiştir — kehanet yok (sınır 14).
   304: `Cache-Control`, `ETag`,
   `X-Content-Type-Options`, `Content-Security-Policy`, `Cross-Origin-Resource-Policy`;
   `Content-Type`, `Content-Disposition`, `Content-Length` ve gövde yok (net/http'nin
   `writeNotModified`'ı da `Content-Type`/`Content-Length`'i siler).
7. **HEAD desteklenmez:** chi'nin 405'i — zincirlerden önce, digest'ten bağımsız; HEAD,
   POST, PUT, DELETE, PATCH, OPTIONS ve TRACE `Allow: GET` ile, tanınmayan `FOO` `Allow`'suz
   (3. tur, `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer`); ölçüldü: panel
   çözümleyicisi 0 kez, okuyucu 0 kez. Tarayıcı `<img>` için GET
   kullanır; T29'un gerekçesi (kimlik çözen rotaya metot eklemenin yarıçapı) aynen.
8. **Global middleware yanıt başlığı eklemez; sayfa CSP'siyle çakışma yok.** `RequestID`,
   `RealIP`, `AccessLog`, `Recoverer`, `Timeout(30 s)`: ölçüldü — `httpx.NewRouter` üzerinden
   gerçek sunucuda 200, 404 ve 304'ün telinde handler'ın başlıkları + net/http'nin `Date`'i,
   başka hiçbir şey. Tel testi `OperatorHost` ayarsız koşar; ayarlıysa `operatorHostOnly` her
   host'ta kurulur ve yalnız o host'un isteklerine etki eder (`internal/httpx/router.go`'daki
   `cfg.OperatorHost != ""` dalı) — o yapılandırmada müşteri host'unun logo yanıtı telde
   ölçülmedi. Logo
   yanıtı `Tap.render`/`AdminAuth.render`'dan geçmez, yani sayfa CSP'si yazılmaz; tek CSP
   rotanın kendi `default-src 'none'; sandbox`'ı. Zincirlerin kendi retleri (303, 429, 500)
   logo baytı taşımaz ve kendi başlıklarıyla döner. `AccessLog` rota desenini yazar
   (`/t/logo/{sha}`), digest değerini değil; rota digest'i ve baytı loglamaz (500'de
   `tenant_id` + hata).
9. **`hasLogo` türetimi — `BrandReader.PageLogo` → `logoRefOf`:** `pgx.ErrNoRows`, bütün
   alanları NULL satır ve logosuz accent'li satır aynı cevaptır (sıfır `LogoRef`, `false`,
   `nil`; WL-1 devri). Başka bir hata hata kalır. Dört logo sütunu (digest, tür, genişlik,
   yükseklik) için okuma tarafının **kendi** hep-ya-hiç kuralı: dördü dolu → logo; dördü
   NULL → logo yok; diğer 14 birleşimin her biri (kutusuz tür, türsüz kutu, digest'siz tür ve
   kutu…) hatadır — tahmin edilen bir türle ya da 0×0 kutuyla çizilmez, sessizce "logo yok"
   da sayılmaz. 00028'in hep-ya-hiç CHECK'i (`tenant_branding_logo_all_or_none`) bu satırları
   tabloya sokmaz, yani dallar o CHECK durdukça ulaşılamaz; kural sayfanın CHECK'e yaslanmaması
   için burada (`TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` 14'ünü de sürer). *(2. tur:
   1. turun kodu digest'siz satırı türü ve kutusu dolu olsa da "logo yok" sayıyordu ve test
   yalnız "digest dolu, gerisi NULL" şeklini sürüyordu; denetçinin X11 — digest + kutu, tür
   NULL → `image/png` sayılır — ve X35 — digest + tür, kutu NULL → 0×0 çizilir — mutantları
   yeşil kaldı. Kural sayıya bağlandı, 14 birleşim türetildi; X11, X35 ve 1. turun kuralı
   (X36) artık kırmızı.)* Bugün çağıranı yok: WL-8/WL-9 bağlar.
10. **Sayfa politikası:** `logoImagePolicy(policy, hasLogo)` — false ise politika bayt bayt
    aynı, true ise `; img-src 'self'` eklenir; `tapCSPFor(hasLogo)`, `adminCSPFor(hasLogo)`,
    transactions bölümü `logoImagePolicy(adminScriptedCSP, hasLogo)`. `hasLogo` "işletmenin
    logosu var" değil "bu yanıt `<img>`'i çiziyor" demektir. Bugün hiçbir render logo çizmez →
    `Tap.render`, `AdminAuth.render`, `renderScripted` üçü de `false` geçer. Giriş ekranları ve
    parola sıfırlama ailesi (ADR 0023 §2: Taptime'ın) dokunulmadı.
11. **Okuyucu ayrı tip** (`BrandReader`, `Brands` değil): sayfa ve görsel istekleri `Save`
    metodu taşıyan bir değer tutmaz. Logo okuması `internal/domain/tenant`'ta olduğu için o
    paketin mevcut belt'i (`TestStaffQueries_CarryAnExplicitTenantPredicate`) `GetTenantLogo`'yu
    türetir — WL-1/WL-4 devri dördüncü bir kopya yazılmadan kapandı;
    `TestBrandRead_TheBeltSeesBothReads` çağrı paketten çıkarsa kırmızı verir.

### Ölçümler — ücretli istek sayısı (§5: sıcak 1, soğuk 2)

Ürünün kurduğu bütçelerle (test kendi limiter'ını kurmaz),
`TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`:

| Kova | Ölçülen |
|---|---|
| tap oturumu (300 / 10 dk) | 299 logo isteği + `GET /t` (300.) → 200; 301. istek, logo da sayfa da → 429 |
| tap adresi (3000 / 10 dk) | aynı adresten 3000 oturumsuz logo isteği → 3000 × 404, 0 oturum çözümlemesi, 0 okuma; ardından oturumlu `GET /t` → 429; aynı oturum başka adresten → 200 |
| panel oturumu (300 / 10 dk) | 299 logo + `GET /admin` (300.) → 200; 301., bölüm de logo da → 429 |
| panel adresi (3000 / 10 dk) | 3000 oturumsuz panel logo isteği → 3000 × 303; ardından oturumlu `GET /admin` → 429; başka adresten → 200 |

Bir logo isteği kendi yüzeyinin adres kovasından **bir**, oturum kovasından **bir** düşer;
kovalar sayfalarınkidir. WL-8/WL-9 logoyu çizince bir sayfa yüklemesi logo önbellekteyken 1,
değilken 2 ücretli istektir (yanıt `private, max-age=31536000, immutable`; sonuç ekranı ve her
panel bölümü aynı URL'i adlandırır). Bütçe aritmetiği güncellendi, **sayılar değişmedi**: tap
(`internal/httpx/ratelimit.go`, `TestTapLimiter_DefaultsAreWideEnoughForAShiftChange`) — 300
kişi × (sayfa + düğme + soğuk logo + bir yeniden deneme = 4) = 1 200 < 3 000; önbelleği kapalı,
5 sn'de bir yenilenen telefon 120 × 2 = 240 < 300. Panel (`adminratelimit.go`) — oturum, görmediği
her logo sürümü için bir kez +1. **Tap'ı engelleyebilir mi:** çerezi tutan biri 300 logo isteğiyle
o oturumun `GET /t` ve `POST /api/checkin`'ini 10 dakika 429'a düşürür — ama bunu `GET /t` ile
de yapabilirdi; mekân ağındaki oturumsuz biri 3000 logo isteğiyle mekânın adres kovasını harcar —
oturumsuz `GET /t` ile de harcayabilirdi (TapLimiter'ın M5-03'ten beri yazılı artığı). **Bütçe
kovaları bakımından** logo rotası yeni bir yetenek eklemez; meşru bir tarayıcı soğuk sayfa başına
en çok bir logo isteği yapar. Çerezsiz istek oturum çözmez (ölçüldü: 3000 istekte 0 `Verify`);
boş olmayan bir oturum çerezi taşıyan istek `Identify`'da bir `Verify` öder — `GET /t` ile aynı.
**Bayt bakımından ekler (2. tur, güvenlik denetimi):** kovalar istek sayar, bayt saymaz; bir logo
yanıtı en çok 262 144 B taşır (00028'in CHECK'i) ve 304'te de DB'den okunur. Canlı bir tap
oturumunun 300 isteği 10 dakikada 300 × 256 KiB = 75 MiB logo çekebilir; panelde aynı aritmetik
oturum başına geçerli ve kayıt herkese açık. Ayrı bir bayt sınırı yok (sınır 12).

### Güvenlik iddiaları — WL-6'nın ölçtüğü parçalar (üç parçalı)

**İddia D (WL-6) — bir işletmenin logosu başka işletmenin oturumuna sunulmaz ve varlığı ayırt
edilemez.**
- **PART I:** `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest` — gerçek okuyucu ve
  gerçek Postgres; ürünün yazıcısıyla (`tenant.Brands`, `brand.LogoGate` çıktısı) saklanan iki
  logo; A oturumunun B'nin digest'ine ve kimsenin saklamadığı digest'e aldığı yanıtlar iki rotada
  da durum, her başlık ve gövdeyle bayt-aynı ve tek 404'e eşit; kontrol: B oturumu B'nin
  digest'ini alır. `TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness` — başka işletmenin
  digest'i, bilinmeyen digest, büyük harfli yazım, değiştirilmiş logonun eski digest'i ve iki
  işletmenin ortak tuttuğu digest'in temizleyen taraftaki okuması tek `ErrLogoNotFound` değeri
  ve metni. `TestLogoRoutes_EveryRefusalIsTheSameNotFound` — testin adlandırdığı sekiz bozuk
  digest, başka işletmeyi adlandıran sorgu dizesi ve tap rotasında çerezsiz, oturum
  adlandırmayan çerezli, iptal edilmiş oturumlu ve yalnız panel çerezli istek aynı 404;
  bozuklar ve oturumsuzlar okuyucuya ulaşmaz; her okuma oturumun işletmesi altında.
  `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession` — panel rotasında yalnız çalışan
  çerezi: 303 → `/admin/login`, çözümleyici 0, okuma 0; çözümleyicinin reddettiği (iptal,
  süresi dolmuş, bilinmeyen — `adminauth.ErrNoSession`) panel oturumu: aynı 303, okuma 0; iki
  işletmeye ait iki çerezle her rota kendi yüzeyinin işletmesini sunar. `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`
  — başka işletmenin ve bilinmeyen digest'te `If-None-Match` (`"<digest>"`, `W/"<digest>"`,
  `*`) 404'ü değiştirmez. `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer` —
  HEAD, POST, PUT, DELETE, PATCH, OPTIONS, TRACE ve `FOO`'nun 405'i kendi ve başka
  işletmenin digest'inde aynı.
- **PART II:** yukarıdaki testler ve `TestStaffQueries_CarryAnExplicitTenantPredicate` (artık
  `GetTenantLogo`'yu türetir) · `TestBrandRead_TheBeltSeesBothReads`. Yakaladıkları,
  mutasyonla (tablo aşağıda): rotanın tenant'ı istekten alması (M01); digest yazım
  kapısının kalkması (M05); bulunamayan digest'e 403 (M06) ya da başka gövdeli 404 (M06b);
  bozuk digest'e başka 404 (M19); iptal edilmiş oturuma (M09) ya da oturumsuz isteğe (M23)
  hizmet; panel rotasının çalışan oturumunu okuması (M10), zincirsiz (M10b) ya da yazma
  zinciriyle (M10c) kurulması; kimliksiz panel handler'ının okuması (M25); `If-None-Match`'in
  aramadan önce değerlendirilmesi (M07); okuyucunun ıskayı digest'le sarması (D04) ya da DB
  hatasını ıskaya çevirmesi (D06); okumanın bu paketten çıkması (D07); `GetTenantLogo`'dan
  tenant yükleminin düşmesi (Q01) ya da `OR true` (Q02) — ikisi yalnız belt'i kırmızıya
  çevirir, izolasyon testleri RLS yüzünden yeşil kalır.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia E (WL-6) — logo yanıtı tarayıcıda belge olarak koşmaz; sayfa img-src'yi yalnız
`<img>` çizdiğinde adlandırır.**
- **PART I:** `TestLogoRoutes_TheHeadersAreExactlyTheADRs` — 200'ün başlık kümesi PNG ve JPEG
  için, iki rotada, §5'in yedisi + `Content-Length` ile birebir; GIF gibi koklanan baytlar
  saklanan `image/png` ile sunulur; tür, dosya adı ve başka işletme adlandıran sorgu dizesi ve
  istek başlıkları hiçbir baytı değiştirmez. `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets`
  — gerçek sunucuda `httpx.NewRouter` üzerinden 200, 404 ve 304: handler başlıkları + `Date`.
  `TestPagePolicies_ALogoWidensByImgSrcAlone` — `tapCSPFor(false)`, `adminCSPFor(false)` ve
  `logoImagePolicy(adminScriptedCSP, false)` bugünkü politikalara (literal) bayt-aynı, `true`
  yalnız `; img-src 'self'` ekler. `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` —
  listelediği her render'da (aşağıda; test listeyi ve sayıyı her koşuda basar) politika
  img-src'yi ancak gövde `<img` içeriyorsa adlandırır; bugün hiçbirinde ikisi de yok;
  yüklemin iki yönde de ayırt ettiği testin kendi kontrollerinde.
- **PART II:** yukarıdaki testler ve `TestTapResponses_CarryTheContentSecurityPolicy` (tap
  politikasının literal'i). Yakaladıkları, mutasyonla: `nosniff` (M02), `sandbox` (M13), CORP
  (M14), `Content-Length` (M12) düşmesi; `Cache-Control`'ün `public` olması (M15);
  `Content-Disposition`'ın sabit `logo.png` olması (M03); `Content-Type`'ın koklanması (M16)
  ya da istekten okunması (M17); 304'ün `Content-Type` taşıması (M20); 404'ün `no-store`'u
  kaybetmesi (M18); img-src'nin her sayfaya (M04a), tap render'larına (M04b), panel
  render'larına (M04c) ya da transactions bölümüne (M04d) eklenmesi; `hasLogo` türetiminde
  satır yok / bütün alanları NULL / logosuz accent'li satırın ayrışması (D01–D03), yarım
  tanımlı logo sütunlarının logo sayılması — türsüz (X11), kutusuz (X35) — ya da digest'siz
  satırın "logo yok" sayılması (X36).
  **img-src testinin kapsadığı render'lar** (2026-10-03, `f3c9c04` tabanında koşu
  *"img-src correspondence over 29 renders (9 panel sections)"* bastı; 1. turun `377daf3`
  tabanında 30/10 idi — aradaki fark OP-10B'nin `/admin/legal`'ı bölüm tablosundan
  çıkarması): `pages.PanelSections`'ın her satırı (imzalı owner fikstürü, `panelBrowser`) ·
  transactions bölümü ve `/admin/dockets` parçası bir kayıtla · imzasız
  `/admin/login` · defter düşmüşken transactions bölümü · `GET /t` · `GET /t` bozuk URL ·
  bilinmeyen plaket · sunucu hatası · onay ekranı `ok`/`flag`/`reject`/`ignored` × practice
  (8) · `POST` bilinmeyen plaket · başka işverenin plaketi · bayat bağlam · tap 429.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

### Mutasyon tablosu

Her satır tek düzenleme; `scratchpad/wl6/mutate.py` dosyayı yedekler, eski metnin tam bir kez
geçtiğini ister, mutantı yazar (sha farkı), testleri koşar, kararı testin kendi çıktısındaki
`--- FAIL` satırlarından okur (derleme hatası ayrı sayılır — hiçbiri derleme hatası değildi),
dosyayı geri yazar ve sha eşitliğiyle doğrular; Q-satırlarında `make sqlc` önce ve sonra
koşar, sonra `db/` ve `internal/store/` diff'i boş. **2. tur:** `f3c9c04` tabanındaki birleşik
ağacın son kod ve test sürümüne karşı hepsi yeniden koşuldu: **44 mutasyon, 44'ü kırmızı**,
derleme hatası 0, geri yazma 44'ünde sha ile doğrulandı. 44 = 1. turun 41'i (D02 ve D03 yeni
`logoRefOf`'un hep-ya-hiç sayımına göre yeniden yazıldı) + X11, X35, X36; ortak 41 satırın
kırmızı test listesi 1. turdakiyle birebir aynı çıktı. **3. tur:** aynı tabanda son kod ve
test sürümüne karşı hepsi bir kez daha koşuldu: **47 mutasyon, 47'si kırmızı** — 2. turun 44'ü
(kırmızı listeleri 2. turdakiyle birebir aynı) + seam testinin taramasını hedefleyen A07, A10,
A08 (üçü yalnız `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` ile koşuldu; üçü de 2. turun
testinde yeşildi — bu turda önce yeşil, tarama sayımı eklendikten sonra kırmızı ölçüldü). İlk koşuda
D06 **yeşil** kaldı (DB hatası ıskaya dönüşüyordu ve hiçbir test bir DB hatasını okuyucudan
geçirmiyordu); `TestBrandRead_ANilTenantOpensNoTransaction`'a başarısız veritabanı kontrolü
eklendi. Q01 ve Q02'de iki izolasyon testi (`TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness`,
`TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`) koşuldu ve yeşil kaldı — RLS satırı
gizler (sınır 8).

| # | Mutasyon | Kırmızı |
|---|---|---|
| M01 | rota tenant'ı istekten alır (`?tenant=`) | `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs` |
| M02 | 200'den `nosniff` düşer | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M03 | `Content-Disposition` sabit `logo.png` | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs` |
| M04a | `logoImagePolicy` img-src'yi her sayfaya ekler | `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`, `TestPagePolicies_ALogoWidensByImgSrcAlone`, `TestTapResponses_CarryTheContentSecurityPolicy` |
| M04b | `Tap.render` `tapCSPFor(true)` (img-src `<img>`'siz tap sayfalarına) | `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`, `TestTapResponses_CarryTheContentSecurityPolicy` |
| M04c | `AdminAuth.render` `adminCSPFor(true)` (img-src `<img>`'siz panel sayfalarına) | `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` |
| M04d | `renderScripted` `true` (img-src transactions bölümüne) | `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` |
| M05 | digest yazım kapısı (`^[0-9a-f]{64}$`) kalkar | `TestLogoRoutes_EveryRefusalIsTheSameNotFound` |
| M06 | oturumun işletmesinde olmayan digest 403 döner | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M06b | okuyucunun ıskası `http.NotFound` (bozuk digest'inkinden başka gövde) | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M07 | `If-None-Match` aramadan ÖNCE değerlendirilir | `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M08 | 304 dalı kalkar (`If-None-Match` yok sayılır) | `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M09 | tap rotası iptal edilmiş oturuma hizmet eder | `TestLogoRoutes_EveryRefusalIsTheSameNotFound` |
| M10 | panel rotası çalışan oturumunu okur (tap zincirinde) | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`, `TestLogoRoutes_AReadFailureIsNotARefusal`, `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M10b | panel rotası `Protect`'siz kurulur | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`, `TestLogoRoutes_AReadFailureIsNotARefusal`, `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M10c | panel rotası `ProtectWriting` arkasında | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`, `TestLogoRoutes_AReadFailureIsNotARefusal`, `TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M11 | tap logo rotasına ayrı bir `TapLimiter` | `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget` |
| M12 | 200'den `Content-Length` düşer | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs` |
| M13 | logo CSP'sinden `sandbox` düşer | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M14 | `Cross-Origin-Resource-Policy` düşer | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M15 | `Cache-Control` `public` | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheHeadersAreExactlyTheADRs`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M16 | `Content-Type` sunumda baytlardan koklanır | `TestLogoRoutes_TheHeadersAreExactlyTheADRs` |
| M17 | `Content-Type` istekten (`?type=`) okunur | `TestLogoRoutes_TheHeadersAreExactlyTheADRs` |
| M18 | 404'ten `no-store` düşer | `TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest`, `TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo`, `TestLogoRoutes_TheWireCarriesWhatTheHandlerSets` |
| M19 | bozuk digest `http.NotFound` alır | `TestLogoRoutes_EveryRefusalIsTheSameNotFound` |
| M20 | 304 `Content-Type` taşır | `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo` |
| M21 | `If-None-Match` güçlü karşılaştırma (`W/` atılmaz) | `TestIfNoneMatch_TheScanFollowsRFC9110`, `TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo` |
| M22 | tap logo rotasına HEAD eklenir | `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer` |
| M23 | tap rotası oturumsuz isteğe hizmet eder (`uuid.Nil` altında okur) | `TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`, `TestLogoRoutes_EveryRefusalIsTheSameNotFound` |
| M24 | çözülmemiş tap kimliği 404 (500 değil) | `TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession` |
| M25 | kimliği canlı olmayan panel isteği okur | `TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession` |
| M26 | okuyucu hatası 404'e döner (500 değil) | `TestLogoRoutes_AReadFailureIsNotARefusal` |
| D01 | `logoRefOf`: satır yok → hata | `TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo`, `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| D02 | `logoRefOf`: bütün alanları NULL satır → logo var | `TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo`, `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| D03 | `logoRefOf`: logosuz accent'li satır "logo yok" değil | `TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo`, `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| D04 | `Logo` ıskayı digest'le sarar (iki metin) | `TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness` |
| D05 | `Logo` nil tenant için transaction açar | `TestBrandRead_ANilTenantOpensNoTransaction` |
| D06 | `Logo` DB hatasını `ErrLogoNotFound`'a çevirir | `TestBrandRead_ANilTenantOpensNoTransaction` |
| D07 | logo okuması bu paketten çıkar | `TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness`, `TestBrandRead_TheBeltSeesBothReads` |
| X11 | `logoRefOf`: digest + kutu dolu, tür NULL → `image/png` sayılır (denetçinin X11'i; denetçi 1. turun testinde yeşil ölçtü) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| X35 | `logoRefOf`: digest + tür dolu, kutu NULL → 0×0 çizilir (denetçinin X35'i; denetçi 1. turun testinde yeşil ölçtü) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| X36 | `logoRefOf`: digest NULL → diğerleri dolu olsa da "logo yok" (1. turun kuralı) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| A07 | seam testi döngüsü `mask < 15` — dolu küme ziyaret edilmez (kapanış denetçisinin A07'si) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| A10 | seam testi döngüsü `mask := 1`'den başlar — boş küme ziyaret edilmez (A10) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| A08 | döngü `< 15` + `logoRefOf` dolu kümeye hata döner (A08) | `TestBrandRead_NoRowAndAnAllNullRowAreOneBranch` |
| Q01 | `GetTenantLogo`'dan tenant yüklemi silinir (parametre korunur) + `make sqlc` | `TestStaffQueries_CarryAnExplicitTenantPredicate`, `TestTenantBranding_EveryStatementNamesTheTenant` |
| Q02 | `GetTenantLogo` yüklemi `OR true` + `make sqlc` | `TestStaffQueries_CarryAnExplicitTenantPredicate` |

Ölçülmeyen (dalı testlerin ulaşamadığı) iki savunma: `BrandReader.Logo`'nun "tür ya da bayt
yok" denetimi ve `logoFileName`'in bilinmeyen türü — ikisi de 00028'in CHECK'leri varken
ulaşılamaz; ikincisi sahte okuyucuyla (`image/svg+xml`) `TestLogoRoutes_AReadFailureIsNotARefusal`'da
500 olarak ölçüldü, birincisi ölçülmedi.

### Sayılı sınırlar (WL-6)

1. **img-src testi yalnız listelediği render'ları görür** (koşu sayıyı basar; 2026-10-03,
   `f3c9c04` tabanında 29 render, 9'u bölüm tablosundan). Aktivasyon ailesi, signup, landing/legal
   (kendi testleri var), operatör yüzeyi, parola sıfırlama, politika onayı/sürüm sayfası, elle
   kayıt, plaket encode ve Account editörü bu listede değil. Yüklem yalnız `<img` görür: CSS
   `url()` görselleri ve SVG `<image>` sayılmaz.
2. **`true` yarısı henüz bir render'da ölçülmedi.** Bugün hiçbir render `hasLogo=true` geçmez;
   `true`'nun doğruluğu `TestPagePolicies_ALogoWidensByImgSrcAlone`'un literal'leri ve
   yüklemin kendi kontrolleriyle sınırlı — ilk gerçek `true` render'ı WL-8/WL-9'undur.
3. **Yönlendiricinin kendi yanıtları farklıdır:** rotayla eşleşmeyen yol şekilleri (boş
   digest, ek segment) chi'nin 404'ünü (`404 page not found\n`, `no-store` yok), GET dışı
   metotlar 405'i alır (ölçülen yedi standart metot `Allow: GET` ile, tanınmayan `FOO`
   `Allow`'suz — karar 7). İkisi de yalnız yolun şekline/metoda bağlıdır; ölçüldü: panel
   çözümleyicisi 0, okuma 0.
4. **DB uçtan uca testinde oturum sahtedir** (işletmeyi adlandırır); oturum → tenant
   çözümlemesi `httpx`/`adminauth`'un kendi testlerinde. 304, HEAD, bütçe ve bozuk digest
   testleri sahte okuyucuyla koşar; okuyucunun tenant sözleşmesi gerçek Postgres'te
   `brandread_db_test.go`'da ölçüldü.
5. **Süre ölçülmedi:** başka işletmenin ve bilinmeyen digest aynı ifadeden geçer (tenant
   birincil anahtarıyla satır, sonra digest karşılaştırması); yanıt süresi kehaneti ölçülmedi.
6. **304 de baytı okur:** `GetTenantLogo` baytı seçer; yalnız varlık okuyan bir sorgu yeni
   sorgu olurdu, eklenmedi.
7. **Devre dışı bırakılmış çalışanın canlı oturumu logoyu alır** (karar 2).
8. **Tenant yükleminin silinmesini yalnız belt yakalar** (Q01, Q02): üretim yolu izolasyon
   testleri RLS yüzünden yeşil kalır. WL-1'in metin pini Q01'i yakalar, Q02'yi (`OR true`)
   yakalamaz (WL-1 notu A07a); `internal/domain/tenant`'ın belt'i ikisini de yakalar.
9. **RFC metni bu görevde okunmadı;** önkoşul sırası ve 304'ün başlıkları go1.27.1
   `net/http/fs.go`'nun (`checkPreconditions`, `writeNotModified`, `scanETag`) okunmasına
   dayanır.
10. **Ölçümler go1.27.1'de;** CI go1.26.6 — telde `Date` dışında başlık eklenmediği 1.26'da
    ayrıca ölçülmedi (CI aynı testi koşar).
11. **Fikstürler dev DB'de kalır** (`tappa_app` `tenant_branding`'de DELETE taşımaz,
    `audit_log` eklemeli; `make db-reset` temizler).
12. **Yanıt başına bayt — ayrı bir sınır yok** (2. tur, güvenlik denetimi). Bütçe kovaları
    istek sayar; bir logo yanıtı en çok 262 144 B taşır ve 304'te de DB'den okunur. Canlı bir
    tap ya da panel oturumu 10 dakikada 300 × 256 KiB = 75 MiB çekebilir; panelde kayıt herkese
    açık. Kapatılmadı, sayıldı; karar WL-7/WL-9'a devredildi.
13. **`OperatorHost` ayarlı yapılandırmada müşteri host'unun logo yanıtı telde ölçülmedi**
    (karar 8); tel testi o ayarsız koşar.
14. **`If-None-Match` satırlarında net/http'den farklı** (3. tur, karar 6): bu rota bütün
    satırları birleştirir, net/http yalnız ilkini okur; [`"x"`, `"<sha>"`] burada 304, net/http'de
    200. Yalnız oturumun kendi logosunu etkiler, sonucu 304'tür, kehanet değildir.

### Devirler

- **WL-7:** Account önizlemesinin `<img>`'i `/admin/brand/logo/{sha}`'dan; o render
  `adminCSPFor(true)`'yu yalnız önizleme `<img>`'i çizdiğinde geçer; önizleme logolu bir
  fikstürle img-src testinin listesine girer. **Bayt (sınır 12):** yükleme rotası gelince ya
  logo okuması için bir bayt bütçesi konur ya da 75 MiB / oturum / 10 dk gerekçesiyle kabul
  edilir (kayıt herkese açık olduğu için panel tarafı burada karara bağlanmalı).
- **WL-8:** panel kabuğu `hasLogo`'yu `BrandReader.PageLogo`'dan (ya da aynı `GetTenantBrand`
  satırından accent'i de okuyan bir genişletmesinden — tek PK okuması) alır; okuma hatası →
  `false` + log (§4.6). `render`/`renderScripted` `hasLogo` taşıyan bir yol ister
  (`adminCSPFor(hasLogo)`, `logoImagePolicy(adminScriptedCSP, hasLogo)`). img-src testi
  logosu olan bir işletmenin bölümleriyle genişler — sınır 2 orada kapanır.
- **WL-8 ve WL-9 — yarım satır ayrı ele alınmalı (3. tur gözlemi, kod değişmedi):**
  `PageLogo` yarım tanımlı bir logo satırına (14 birleşim) "logo yok" değil bir **hata**
  döndürür, ve bu hata `ErrLogoNotFound` DEĞİLDİR. Denetçinin A04'ü — yarım satıra
  `ErrLogoNotFound` döndürmek — WL-6'nın testlerinde fark edilmiyor, çünkü bugün
  `PageLogo`'nun çağıranı yok. Bir çağıran hatayı `errors.Is(err, ErrLogoNotFound)` ile
  "logo yok"a çevirirse (ya da A04 olur ve çağıran öyle yazılırsa) yarım satır sessizleşir:
  sayfa logosuz render edilir, log satırı yazılmaz. Çağıran yarım satırı (`PageLogo`'nun
  hata dönüşü) okuma hatası gibi ele alıp loglamalı (§4.6) ve bunu kendi testinde sürmeli.
- **WL-9:** tap ve sonuç ekranı `hasLogo`'yu `TapPage`'in transaction'ında okur;
  `ErrForeignLocation` → `false` (ADR 0023 §2 uyuşmazlık kuralı); `Tap.render` render başına
  `tapCSPFor(hasLogo)`; `<img src="/t/logo/{sha}">` `width`/`height` ile; sıcak/soğuk
  aritmetiği gerçek sayfayla yeniden ölçülür
  (`TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget`'ın deseni). **Bayt (sınır 12):** tap
  sayfası logoyu çizmeye başlayınca tap tarafındaki 75 MiB / oturum / 10 dk ya bir bayt
  bütçesiyle sınırlanır ya da gerekçesiyle kabul edilir.
- **WL-10:** denetim listesine İddia D ve E'nin WL-6 parçaları, mutasyon tablosu, sayılı
  sınırlar.
- **WL-12 / orkestratör:** İddia D ve E PART I'in *"WL-6'da ölçülecek"* yarıları ölçüldü —
  yukarıdaki notlara işaret eden bir satır önerilir; m10 §5 "Servis" maddesine
  `Content-Length` ve 304 kararı.
