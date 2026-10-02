# ADR 0024 — Kullanıcının yüklediği görsel (tenant logosu): PNG/JPEG, decode'dan önce sınır, yeniden kodlama, `bytea`, oturumdan tenant

- **Durum:** kabul edildi — White-label **K5** (PNG/JPEG) ve **K7** (Postgres `bytea`),
  *"✅ önerisiyle uygulanır"* ([m10-platform.md](../plan/m10-platform.md) §6); tap ekranındaki
  logonun kendisi kullanıcı kararı **D-C** (2026-09-24, [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) §7).
  **Uygulama: yok** (HEAD `c0c0250`'de üretim kodunda görsel paketi import eden, multipart
  okuyan ya da şablonda `<img>` render eden yer 0 — aşağıda ölçüldü); uygulama WL-1, WL-3,
  WL-4, WL-6, WL-7, denetim WL-10.
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
| Ingress `proxy-body-size: "1m"`; yorumu *"Every request this product accepts is a form post or a small JSON body"* diyor — logo yüklemesiyle bu önkabul güncelliğini yitirir. İstek tamponlama için annotation yok (ingress-nginx varsayılanı; kümede **ölçülmedi**) | `deploy/k8s/40-ingress.yaml:127-129` |
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
ister ve CI 1.26.x koşar; bu sayılar CI araç zincirinde yeniden ölçülmedi — WL-3'ün testleri
onları CI'da yeniden üretir. Fikstürler ImageMagick ve elle kurulmuş başlıklarla üretildi.

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
     eşzamanlı en kötü decode altında **RSS**'i ölçer ve karta yazar. Tek replika, yani OOMKill
     tap dahil ürünün durması demektir (`20-app.yaml:53`).
   - **CPU — kural ve sonucu:** N < `GOMAXPROCS` (bir yuva bir çekirdeği tutar; en az bir
     çekirdek tap yoluna kalmalı). `GOMAXPROCS`'un kaynağı: Go 1.25 ve sonrasının çalışma zamanı
     Linux'ta cgroup CPU sınırını okur ve varsayılanı mantıksal CPU sayısı ile o sınırın küçüğüne
     indirir; `go.mod` `go 1.26.2` (davranış varsayılan açık; depoda `GOMAXPROCS` ayarı ve
     `godebug` yönergesi 0). Düğüm 16 CPU (`10-postgres.yaml:208`), sınır `"2"`
     (`20-app.yaml:502`) → `GOMAXPROCS` = 2 (türetildi, podda ölçülmedi) → **N = 1 kuralın
     sonucudur**, öneri değil. Kabul: WL-3 pod içinde `runtime.GOMAXPROCS(0)`'ı bir kez ölçer ve
     karta yazar; 2'den farklıysa N yeniden hesaplanır.
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
  404, bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu 404).
- **Başlıklar** (→ **WL-6**: birebir):
  `Content-Type` = saklanan mime (sunum anında koklanmaz) · `X-Content-Type-Options: nosniff` ·
  `Content-Security-Policy: default-src 'none'; sandbox` (URL doğrudan açılırsa) ·
  `Cross-Origin-Resource-Policy: same-origin` · `Content-Disposition: inline;
  filename="logo.png"` ya da `"logo.jpg"` — **mime'a göre** (tasarım özü sabit `logo.png`
  yazıyordu; JPEG için yanlış uzantı olurdu) · `Cache-Control: private, max-age=31536000,
  immutable` (içerik adresli) · `ETag: "<sha>"`.
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
  - **tenant başına yükleme bütçesi** (pencere başına sayı ve bayt);
  - gövde okunmadan önce **eşzamanlı yükleme kabul sınırı** (dolu ise gövde okunmadan ret);
  - gövde okuması için `http.NewResponseController(w).SetReadDeadline`;
  - ingress bağımlılığı adıyla yazılır: `40-ingress.yaml:127-129`'un *"small JSON body"*
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
  HTML) kabul edilmesi; istemcinin `Content-Type`'ının ya da dosya adının sonucu değiştirmesi.
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
  kez ölçülecek (pin değil): 512Mi konteynerde N eşzamanlı en kötü decode altında RSS.
- **PART II:** WL-3'in bomba testi (tahsis ölçümüyle) · tarama tavanı testi (dürüst olmayan
  vakayla) · kesik girdi testi · semafor testi (iptal vakasıyla) · fuzz testi. Yakaladıkları:
  `DecodeConfig` kapısından önce `Decode` çağrılması; tarama sayımının kaldırılması ya da
  çözücüden az sayan bir sayıma dönüşmesi; yeniden decode'un sınır dışı bir çıktıyı kabul
  etmesi; semaforun atlanması ya da iptalde erken bırakılması; çözücüde panik.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C — saklanan bayt girdinin meta verisini ve ek yükünü taşımaz.**
- **PART I:** S11–S14 (bugün ölçüldü). WL-3'te ölçülecek: EXIF-GPS'li JPEG'in çıktısında
  `Exif` APP1 yok; IEND sonrası yük çıktıda yok; çıktının bölüm listesi S14'teki kümedir.
- **PART II:** WL-3'in meta veri testleri (JPEG APP1/COM, PNG `eXIf`/`tEXt`/`iCCP`, IEND ve EOI
  sonrası bayt) · çıktı bölüm listesi testi. Yakaladıkları: yüklenen baytın saklanması;
  kodlayıcının meta veri yazan bir yolla değiştirilmesi.
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

1. **Ölçümler go1.27.1'de.** CI 1.26.x; WL-3 testleri sayıları CI'da yeniden üretir.
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

## Karar verilmedi

- JPEG tarama tavanının sayısı ve tavanın süre bütçesi (doğal aday router'ın 30 s'si) — WL-3.
  (Semafor N karara bağlı değil, kuralın sonucu: N = 1 — §2.6.)
- GC: `GOMEMLIMIT` mi, `2 × (N × tepe + taban)` hesabı mı (§2.6) — WL-3, RSS ölçümüyle.
- Progressive / 4 bileşenli JPEG'i tamamen reddetme alternatifi (§2.6; 96 → ~64 MiB) — WL-3
  ölçümle seçer.
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
