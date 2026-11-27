# Taptime işareti ve kelime markası

> Durum: **seçildi, vektör master hazır** (2026-11-27). Üretim planı: [PLAN.md](PLAN.md).
> Görsel dilin geri kalanı: skill `tappa-brand`. Bu dosya yalnız işareti anlatır.

## İşaret: `t` ve nokta yerine plaket

`t▯`: Space Grotesk 700 `t`, nokta yerine **plaket**. Plaket, duvardaki NFC plaketidir:
yuvarlak köşeli dikey bir dikdörtgen, üst kısmında telefonun değdiği **halka**. Halka bir **delik**tir;
tek renkli ve ters sürümler bu sayede ek bir renge ihtiyaç duymadan doğru çıkar.

Neden bu yön:

- Ürünün zaten taşıdığı iki parçayı tek işarete bağlar: panelin dar rayındaki `t.` (`PanelWordmark`)
  ve landing'deki halkalı yeşil disk (`.lp-markDot`). Yeni bir şekil öğretmez.
- Ürünü anlatır ama klişeye düşmez: NFC dalgası, telefon, parmak izi, saat ve tik işareti yoktur.
  "Duvarda bir plaket, ona dokunulur" bilgisi plakette ve halkada durur.
- 16 px'te `t.` diye okunur, plaket nokta olarak görünür.
- Kelime markası aynı parçayla biter: `taptime▯`. İşaret ile kelime aynı noktayı paylaşır, bu yüzden
  yan yana konduklarında `t. taptime` diye tekrar etmezler.

Konsept süreci (özet): Nano Banana Pro ile 120 işaret konsepti üretildi. Bunlar PLAN §3'teki dört yönü ve
altı ek yönü kapsıyordu: kaşe, `t.`, delik, kapı, temas ve rafine turlar. Sahip "NFC plaketi ile T birlikte"
istedi. Bunun üzerine plaket ve t birleşimleri elle vektör olarak çizildi; dört aile (A–D) çıktı. Seçilen
B ailesi, son turdaki altı varyant arasında V1'dir: kısa plaket, üstte halka. AI çıktıları repoya girmez,
işaret elle kurulmuştur.

## Geometri (font birimi: punto 1000, taban çizgisi y = 0)

| Parça | Değer |
|---|---|
| `t` | Space Grotesk `wght=700` glifinin dış hatları. Sınır x 26–412, yükseklik 650, gövde 126, x-yüksekliği 486 |
| Harf ile plaket arası | 58 |
| Plaket | genişlik 168, yükseklik 300 (x-yüksekliğinin ~0,62'si), köşe 46, tabanı taban çizgisinde |
| Halka | merkezi plaketin üstünden 80 aşağıda, dış yarıçap 56, iç yarıçap 34 (kalınlık 22). Delik: `fill-rule="evenodd"` |
| Kelime markasında plaket | `taptime` (700, harf aralığı −0,03 em, HarfBuzz kerning) + son harften 28 birim sonra aynı plaket |
| Etiket | `PUNCHLESS`, IBM Plex Mono 400, kelimenin 0,34'ü punto, harf aralığı 0,12 em, ink %70 |

Yazılar SVG'de **dış hat**tır. Fonta bağımlı değildirler ve her makinede aynı görünürler.

## Renkler

Yalnız palet: ink `#152219`, porcelain `#EDF0EA`, paper `#FFFDF4`, tappa-green `#1F5C41`.
Saffron ve tomato durum renkleridir, işarete girmez. Gradient, gölge, cam ve 3B kullanılmaz.

| Sürüm | Kullanım |
|---|---|
| Renkli: ink harf + yeşil plaket | açık zemin (porcelain, paper) |
| `-ink`: tamamı ink | tek renk baskı, faks, gravür |
| `-paper`: tamamı paper | ink ya da yeşil zemin |
| İkon: yeşil karo + paper işaret | uygulama ikonu, favicon, PWA |
| Encode aracı ikonu: ink karo + paper işaret | personel uygulamasından ayırt edilsin diye |

## Küçük boy kuralı

- **32 px ve altı halkasızdır** (`-small`). Bu boyda halka 1 pikselin altına düşer ve gri bir lekeye döner.
- `favicon.ico` 16 px katmanı **elle piksel piksel** çizilmiştir: gövde 2 px, çubuk 2 px, plaket 3×5.
  32 ve 48 px katmanlarında gövde tam piksele oturtulmuştur; halka 48 px'te geri gelir.
- En küçük kullanım: işaret 16 px, kelime markası 72 px genişlik.
- Boşluk: işaretin her yanında en az plaketin genişliği kadar boşluk bırakılır.

## Yapma

- Plaketi ayrı bir logo gibi tek başına kullanma; `t` olmadan bir NFC etiketi resmine döner.
- Halkayı doldurma, ikinci bir halka ya da dalga ekleme.
- Plaketi başka harfin yanına koyma (`taptime▯` dışında).
- `t`'yi başka bir fontla yeniden çizme. Dış hat master'dan gelir.
- İşareti döndürme, eğme, gölgeleme.

## Dosyalar

`svg/`, master'lardır. Bütün türevler bunlardan üretildi:

| Dosya | Ne |
|---|---|
| `taptime-mark{,-ink,-paper}.svg` | işaret (şeffaf zemin) |
| `taptime-mark-small.svg` | halkasız işaret (≤ 32 px) |
| `taptime-wordmark{,-ink,-paper}.svg` | `taptime▯` |
| `taptime-lockup-horizontal{,-ink,-paper}.svg` | `taptime▯  PUNCHLESS` (masaüstü başlığı gibi) |
| `taptime-lockup-stacked{,-ink,-paper}.svg` | `taptime▯` üstte, `PUNCHLESS` altta (mobil başlık ve panel gibi) |
| `taptime-icon.svg` | 1024 karo, uygulama ikonu ve PWA |
| `taptime-icon-small.svg` | halkasız karo: `favicon.svg` |
| `taptime-icon-maskable.svg` | PWA maskable (güvenli daire %80) |
| `taptime-icon-encode.svg` | encode aracı (ink karo) |

Türevler: `web/static/` favicon seti, `mobile/ios/Taptime/Assets.xcassets` (AppIcon, LaunchMark,
LaunchBackground), `mobile/android` ve `android` adaptive + monochrome ikon, `mobile/android` Android 12+
splash.

Üretim zinciri **repo dışında**, yerel bir çalışma klasöründe durur. Kullanılanlar: Python fontTools ve
HarfBuzz ile glif dış hatları, `rsvg-convert` ile PNG/PDF, Pillow ile ICO. **Node kullanılmadı.** Fontlar
OFL'dir; dış hatları alınmış harfler bir logo olarak kullanılabilir.
