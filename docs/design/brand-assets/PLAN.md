# Taptime marka görselleri — üretim planı

> Durum: **1a–1c TAMAM** (2026-11-27): işaret seçildi (`t` + plaket nokta, B ailesi V1), vektör master `svg/`, kullanım
> [README.md](README.md). 2–8 türevleri dal `brand/taptime-logo`'da. (Plan ilk yazıldığında, 2026-11-25: üretim başlamamıştı.)
> Kurallar: konsept görselleri bir AI görsel aracıyla (Nano Banana Pro) repo dışında üretilir; ücretli üretim
> (video, ücretli SVG modeli dahil) yalnız kullanıcının **o anki** açık onayıyla. Araç ve hesap ayrıntısı, iş
> kimliği, kuyruk ekran görüntüsü repoya girmez (repo **public**).

Kullanıcıya görünen marka **Taptime** (iç kod adı Tappa, görsellerde yazmaz). Görsel dil: skill `tappa-brand`.

## 0. Bugün ne var (ölçüldü, 2026-11-25, `81dfa043`)

| Yüzey | Durum |
|---|---|
| Logo | Görsel dosya yok. İşaret CSS ile çizilmiş yeşil disk + halka (`.lp-markDot`), yanında Space Grotesk `taptime` + mono `PUNCHLESS`. Tap ekranı, panel (`PanelWordmark`), plaket çizimi aynı kelime markasını metin olarak çizer. |
| Favicon / apple-touch / manifest | **Hiç yok** — `base.templ`'de `rel="icon"` 0. |
| OG kartları | 9 adet, 1200×630, `web/static/og/` — HTML panodan headless Chrome ile çiziliyor, `TestShareImages_AreDrawnFromThePinnedSources` PNG + kaynak sha256'sını pinliyor. Görünüm temiz, eski işareti taşıyor. |
| Landing görselleri | Yok, bilinçli: `marketingCSP`'de `img-src` yok, illüstrasyonlar satır içi SVG. Tek istisna `/` demo videosu + `video/demo-poster.jpg` (gömülüyse; repoda dosya yok). |
| Mobil — çalışan uygulaması (`mobile/android`, `mobile/ios`) | Android: adaptive icon, arka plan `tappa_green`, ön plan geçici vektör (kâğıt kare + halka); monochrome katman yok; splash tanımı yok (Android 12 varsayılanı). iOS: **asset catalog yok, AppIcon yok**, `UILaunchScreen` boş sözlük. |
| Mobil — plaket encode aracı (`android/`, `mt.taptime.relay`) | Aynı geçici vektör ikon. |

## 1. Kesin gerekenler

AI yalnız **konsept** aşamasında (1a) kullanılır. Geri kalan her şey seçilen işaretin vektöründen **türetilir**:
ikonda, faviconda, splash'ta piksel doğruluğu ve 16 px okunurluk gerekir, palet kilitlidir, AI tipografisi
güvenilmez.

| # | Çıktı | Nasıl | Nereye |
|---|---|---|---|
| 1a | **Logo işareti konseptleri**: 4 yön × 4–6 varyant | Nano Banana Pro (§3 promptları) | repo dışı çalışma klasörü |
| 1b | **Seçilen işaretin vektörü** (SVG master: renkli, tek renk ink, ters paper) | Geometrik işareti elle SVG'ye çevirme. Olmuyorsa ücretli vektörleştirme **yalnız onayla**. | `docs/design/brand-assets/` |
| 1c | **Kelime markası kilidi**: işaret + `taptime` + `PUNCHLESS` (yatay, dikey, yalnız işaret) | Gerçek Space Grotesk / IBM Plex Mono ile kurulur, AI harf çizmez | aynı yer |
| 2 | **Favicon seti**: `favicon.svg`, `favicon.ico` (16/32/48), `apple-touch-icon.png` 180, `icon-192.png`, `icon-512.png`, `icon-512-maskable.png` | 1b'den render | `web/static/` |
| 3 | **iOS AppIcon** 1024×1024, alfa yok | 1b'den | `mobile/ios/Taptime/Assets.xcassets/AppIcon.appiconset` (yeni asset catalog) |
| 4 | **iOS launch screen**: porcelain zemin + ortada işaret, yazı yok | `UILaunchScreen` → `UIColorName` + `UIImageName` (vektör PDF/SVG asset) | aynı catalog + `Info.plist` |
| 5 | **Android adaptive icon**: foreground (108dp, işaret 66dp güvenli alanda), background, **monochrome** (Android 13 temalı ikon) | 1b'den vector drawable | `mobile/android/.../res/` |
| 6 | **Android 12+ splash**: `windowSplashScreenBackground` porcelain, `windowSplashScreenAnimatedIcon` = işaret (2/3 daire içinde), animasyon yok | 1b'den | `themes.xml` + drawable |
| 7 | **Encode aracı ikonu**: aynı işaret, ayırt edilsin diye **ink** zemin | 1b'den | `android/.../res/` |
| 8 | **OG kartları yenilemesi**: 9 kart yeni işaretle | Mevcut boru hattı: pano kaynağında işaret değişir → headless Chrome → sha256 pinleri birlikte güncellenir | `web/static/og/` |

"Temiz" ölçütü: palet dışı renk yok (ink `#152219`, porcelain `#EDF0EA`, paper `#FFFDF4`, tappa-green `#1F5C41`;
saffron ve tomato **durum renkleridir**, logoya girmez), gradient / gölge / cam / 3B parlaklık yok, splash'ta metin
yok, 16 px'te okunur.

## 2. Kullanıcı kararı isteyenler (AI ile üretilir, ama önce evet lazım)

| # | Çıktı | Fayda | Bedeli / çatışması |
|---|---|---|---|
| 9 | **Ürün sahnesi fotoğrafları**: duvardaki plakete telefon değdiren el. Mutfak, bar (gece), üretim sahası (eldivenli el). **Yüz yok, yalnız eller** ("No fingerprints, no face" mesajıyla uyumlu). 16:9 + 1:1 + 4:5 | OG kartlarının sağ yarısı, landing hero, mağaza görselleri; şu an fiziksel ürünü gösteren hiçbir görsel yok | (a) `tappa-brand` "stok illüstrasyon yok" diyor, bilinçli istisna kararı gerekir. (b) Landing'e `<img>` girmesi `marketingCSP`'ye `img-src 'self'` ve testlerde değişiklik demek (güvenlik sınırı → ADR). (c) Plaket henüz basılmadı (M8); görsel plaket tasarım önerisine uymalı. AI QR ve yazıyı doğru basamaz, o alanlar boş üretilir. |
| 10 | **Play Store feature graphic** 1024×500 | Play listesi için **zorunlu** | 9'dan türetilir ya da düz marka kompozisyonu olur; mağaza yayını (M16 Faz F) ayrı karar |
| 11 | **App Store / Play promo görselleri** | — | Mağaza **ekran görüntüleri gerçek uygulamadan** alınır, AI değil (Apple 2.3.3). Yalnız çerçeve/başlık bandı tasarlanır. |

## 3. Konsept promptları (Nano Banana Pro)

Ortak gövde (her konseptin başına):

> Minimal flat vector logo symbol for "Taptime", a time-and-attendance service where staff tap their own phone on a
> small plaque mounted on the wall. Symbol only — no letters, no text. Geometric, solid fills, single consistent
> stroke weight, no gradient, no shadow, no 3D, no glass, no glow. Strict palette: deep green #1F5C41 and ink
> #152219 on porcelain #EDF0EA background; paper #FFFDF4 allowed as an inner fill. Must stay legible at 16 px.
> Centered with generous margin, square 1:1, presented on a plain background.

| Yön | Ek cümle |
|---|---|
| **A — Dokunma noktası** (mevcut işaretin evrimi) | A solid green disc with a single concentric inner ring — the exact spot on the plaque where the phone touches. Explore ring thickness and the gap between them; one variant with a small offset arc suggesting contact. |
| **B — Plaket** | A slightly rounded vertical plaque shape; its upper part holds a ring (the touch spot), lower part empty. Calm, architectural, like a wall fixture. |
| **C — t-halkası** | A lowercase "t" built from geometry: its crossbar or top terminal forms a ring, as if the letter itself is the touch point. Avoid any wifi or radio-wave icon. |
| **D — Adisyon kenarı** | A circle or square whose bottom edge is perforated like a kitchen docket / receipt tear-off, with a ring inside. Restaurant-ticket feeling, not a generic badge. |

Negatif (her prompta): `no wifi symbol, no fingerprint, no face, no clock face, no checkmark, no gradient, no
mockup, no text`. NFC dalgası, saat, tik ve parmak izi klişe; parmak izi ayrıca ürünün tam tersi.

Seçim ölçütü: 16 px / 32 px küçültme testi, tek renk (ink) hâli, porcelain ve ink zeminde okunurluk, kelime
markasıyla yan yana denge.

**Ürün sahnesi taslağı (yalnız #9'a evet gelirse):**

> Photorealistic editorial product photo: a hand holding a smartphone flat against a small matte off-white (#FFFDF4)
> plaque mounted on a [tiled restaurant-kitchen wall beside a doorway | dark wooden bar wall at night, warm light |
> painted factory wall, the hand in a work glove]. The plaque shows one dark green (#1F5C41) printed ring in its
> upper part and nothing else — no text, no QR, no logos. No faces, no people beyond the hand. Soft natural light,
> shallow depth of field, calm and clean, muted deep-green and warm off-white palette. 16:9.

## 4. Akış (başla denince)

1. Üretim kayıtları (prompt, seçim) repo dışındaki çalışma klasöründe tutulur.
2. Bu listede onay: 1–8 kesin; 9–11 için evet/hayır.
3. 1a konseptleri → kullanıcı en fazla 2 yön seçer → bir tur daha → tek işaret.
4. 1b–1c vektör master. Ücretli model gerekirse önce sorulur.
5. 2–8 türetilir. Koda giren kısımlar (favicon `<link>`'leri, OG pinleri, Android tema, iOS asset catalog)
   CLAUDE.md §10 iş moduyla görev görev ilerler (uygulayıcı ajan + ayrı 3. göz).

## 5. Koda dokunmadan önce bilinmesi gerekenler

- **Logo değişimi tap ekranına ulaşır.** `layout.BrandHeader` / Wordmark tap ve aktivasyon başlığını çizer, bu
  §9 kutsal ekranıdır: **önce sor**. `TestUnbrandedScreens_AreByteIdenticalToTheGolden` 37 golden'ı yeniden
  yazdırır.
- **Favicon `<link>`'i `documentHead`'e eklenirse** tap/aktivasyon head'leri değişir, yani yine golden ve §9
  sorusu. Alternatif: favicon yalnız marketing + panel kabuğuna eklenir, tap ekranı tarayıcının `/favicon.ico`
  isteğine kalır (link'siz, golden değişmez).
- **CSP:** sayfaların çoğunda `default-src 'none'` var, `img-src` yok. Favicon isteğinin bu politikaya
  takılıp takılmadığı tarayıcıya göre değişebilir (Firefox'ta takıldığı biliniyor, **ölçülmedi**). Uygulamadan
  önce Chrome ve Firefox'ta ölçülecek. Takılıyorsa kapatmak `img-src 'self'` gerektirir. Güvenlik
  sınırı değişikliğidir: ADR + `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse` ve
  `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`.
- **OG pinleri:** PNG ile düzeltilmiş kaynak birlikte yenilenir (`shareCardBoardEdits`,
  `internal/handler/sharecards_test.go`). Görselde fiyat yok, kişiler uydurma.
- **Node yok:** render/dönüştürme için Node tabanlı araç (svgexport, sharp vb.) kullanılmaz. Headless Chrome,
  `rsvg-convert`/ImageMagick (yerel, repo dışı) ya da Xcode/Android araçları kullanılır.
- Tenant'ın kendi favicon'u kapsam dışı (ADR 0023 §9). Bu plan yalnız Taptime'ın kendi ikonudur.
