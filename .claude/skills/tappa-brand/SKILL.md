---
name: tappa-brand
description: Tappa'nın görsel dili — palet, tipografi, "kitchen docket" motifi, kaşe damgaları, Tailwind token'ları ve templ bileşen kuralları. HERHANGİ bir arayüz işine başlamadan önce oku: dashboard, tap ekranı, landing, portal, e-posta şablonu, PDF/CSV rapor başlığı. "Ekran yap", "sayfa tasarla", "bileşen ekle", "stil ver", "renk seç" tipi her istekte geçerli.
---

# Tappa marka sistemi

Tappa mutfaklarda, üretim tesislerinde, barlarda kullanılıyor. Arayüz **temiz,
sakin ve hızlı okunur** olmalı — yağlı elle, kötü ışıkta, 3 saniyede. Süsleme yok.

## Palet

| Token | Hex | Kullanım |
|---|---|---|
| `ink` | `#152219` | metin, koyu yüzey |
| `porcelain` | `#EDF0EA` | sayfa zemini |
| `paper` | `#FFFDF4` | kart/adisyon zemini |
| `tappa-green` | `#1F5C41` | birincil aksiyon, APPROVED |
| `green-lite` | `#E1EDE6` | onaylı satır zemini |
| `saffron` | `#D98E2B` | uyarı, FLAGGED, geç kalma |
| `saffron-lite` | `#F7EBD6` | flagged satır zemini |
| `tomato` | `#BE3D2A` | REJECTED, yıkıcı aksiyon |
| `line` | `#C9D2C8` | kenarlık, ayraç, perforasyon |

**Palet dışına çıkma.** Renk gerekiyorsa mevcut token'ın opaklığını kullan
(`bg-tappa-green/10`), yeni hex uydurma. Gradient yok. Neon yok.

**Durum → renk eşlemesi sabittir** ve asla ters çevrilmez:
`ok/APPROVED → tappa-green` · `flag/FLAGGED → saffron` · `reject/REJECTED → tomato` ·
`ignored → line (gri, sönük)` · `TRAINING → ink üstüne kesikli çerçeve`.

⚠️ **Renk NEREYE uygulanır — kullanıcı kararı, 2026-08-01.** Eşleme yukarıdaki gibi
kalır, ama durum rengi **kelimeye değil çerçeveye** girer: kaşe damgasının **metni
her zaman `ink`**, durum rengi **kenarlığı, iç halkayı ve %10'luk zemin tonunu**
taşır. Sebebi ölçüm: metin durum renginde iken damgaların ikisi AA'nın altındaydı
(aşağıdaki tablo). Bu kural **damga için** yazılmıştır; `Notice` gibi bileşenlerde
renk zaten kenarlıkta ve zeminde, metin `ink` — orada değişen bir şey yok.

## Tipografi

- **Space Grotesk** — başlık, buton, marka. Sıkı harf aralığı (`tracking-tight`).
- **IBM Plex Mono** — her sayı: saat, süre, tag UID, sayaç, güven puanı, CSV.
  Bir veri hücresi mono değilse yanlıştır; adisyon hissi buradan gelir.
- **Kural:** Google Fonts'a (ya da herhangi bir dış kaynağa) runtime bağlantı
  **yok** — GDPR + çevrimdışı çalışma. Bu kural tutuluyor: render edilen sayfada
  `href`/`src` yalnız `/static/…` yollarını gösterir, mutlak URL sayısı **0**.
- ✅ **Durum (2026-07-31, M5-04):** yazı tipleri artık **self-host EDİLİYOR.**
  `web/static/fonts/` altında Space Grotesk (variable 300–700) ve IBM Plex Mono
  (400/700), her biri `latin` **ve** `latin-ext` alt kümesiyle; altı woff2
  toplam **79.032 bayt** (~77 KiB — dizinin tamamı 92.126 bayttır, farkı iki OFL
  metni ve README oluşturur ve tarayıcı onları indirmez), Go ikilisine gömülü,
  `input.css`'te 6 `@font-face` ile tanımlı.
  `latin-ext` şart: Maltaca **ċ ġ ħ ż** (arayüz metni İngilizce ama çalışan ve
  mekân **adları** değil). Dosyalar derleme zamanında bir kez indirildi ve
  commit edildi; kaynak URL'ler, sha256'lar ve **SIL OFL** lisans metinleri
  `web/static/fonts/README.md`'de. (Bu satır M5-02 sonunda "self-host edilmiyor"
  diyordu ve o zaman doğruydu — dizin yoktu, `@font-face` = 0.)
  **Yeni bir ağırlık/aile eklerken:** dosyayı `web/static/fonts/`'a koy,
  README'nin tablosuna kaynak + boyut + sha256 yaz, lisansı doğrula. `@font-face`
  içinde **uzak URL kullanma** — bu dosyaların var olma sebebi o kırmızı çizgi.

## İmza motif: kitchen docket

Her işlem kaydı bir **mutfak adisyonu fişi**dir. Bu, ürünün tanınma işareti —
tablo satırına çevirme.

Anatomi:
- `bg-paper`, keskin köşe (`rounded-none`) veya en fazla `rounded-sm`
- Üst ve alt kenarda **perforasyon**: `line` renginde tekrarlayan yarım daireler
  (CSS `radial-gradient` ile; görsel dosya kullanma)
- Tek `line` kenarlık, çok hafif gölge (`shadow-sm`) — yükseltilmiş kart değil
- İçerik mono, sola dayalı, dar satır aralığı; etiketler küçük ve `uppercase`
- **Kaşe damgası**: sağ üstte hafif eğik (`-rotate-6` … `-rotate-12`), **çift
  çerçeveli** (2px kenarlık + 1px iç halka), harf aralığı geniş, 11px bold
  uppercase. Mürekkep izlenimi — düz badge değil.
  **Anatomi (2026-08-01'den beri):**
  - **kelime** → her zaman `text-ink`. Durumu gören kullanıcıya taşıyan şey budur.
  - **2px kenarlık + 1px iç halka** (`box-shadow: inset 0 0 0 1px var(--stamp-tone)`)
    → **durum rengi**. Mürekkep izlenimi buradan gelir.
  - **zemin** → `bg-<token>/10`, aynı durum renginin %10'u.
  - **`opacity-80` KALDIRILDI** (2026-08-01). Grup opaklığı içindeki her şeyi
    soluklaştırıyordu ve rengi taşıyan artık çerçeve: saffron kenarlığı paper
    üstünde 2,62:1 → **2,14:1**'e, `line` kenarlığı 1,52:1 → **1,39:1**'e
    düşüyordu. Kelime her hâlükârda AA'yı geçiyordu (`ink@.8` on paper =
    **8,54:1**, ölçüldü) — yani bu karar **metnin değil çerçevenin** okunurluğu
    için verildi.

  **Ölçülen kontrast (WCAG bağıl parlaklık, sRGB kompozit).** Damga `.docket`'in
  içinde, yani zemin `paper #FFFDF4` (üretilen HTML'den doğrulandı: `<span
  class="stamp …">` `<section class="docket">` ile `</section>` arasında ve arada
  zemin veren başka eleman yok). AA, 11px bold için **4,5:1** ister — 11px "large
  text" DEĞİLDİR.

  | Damga | ÖNCE: kelime rengi | ÖNCE @`opacity:.8` | SONRA: kelime `ink` / zemin `<token>/10` |
  |---|---|---|---|
  | `stamp--approved` (tappa-green) | 7,73:1 ✅ | 4,70:1 ✅ | **13,85:1** ✅ |
  | `stamp--flagged` (saffron) | 2,62:1 ❌ | 2,14:1 ❌ | **14,81:1** ✅ |
  | `stamp--rejected` (tomato) | 5,30:1 ✅ | 3,77:1 ❌ | **13,99:1** ✅ |
  | `stamp--ignored` (line) | 1,52:1 ❌ | 1,39:1 ❌ | **15,55:1** ✅ |
  | `stamp--training` (ink) | 16,17:1 ✅ | 8,54:1 ✅ | **13,27:1** ✅ |

  🔴 **Bu skill kendi "Kontrast AA" kuralını çiğniyordu** — "hep böyleydi" değil,
  **yanlıştı**. Kural aşağıda (§ templ + Tailwind) yazılıyken, beş damganın
  **gerçekte render edilen** hâlleri (yani `opacity:.8` uygulanmış hâlleri) sayıldığında
  **üçü** AA'nın altındaydı: `ignored` 1,39 · `flagged` 2,14 · `rejected` 3,77.
  (`approved` 4,70 ile sınırın hemen üstünde, `training` 8,54 rahat geçiyordu.
  Opaklık uygulanmadan sayılırsa **ikisi** altında: `ignored` 1,52 · `flagged` 2,62.)
  Skill hem eşlemeyi hem AA'yı emrediyor, ama ikisinin **çakıştığını** hiç
  ölçmemişti; kusur 2026-07-31'de M5-06 denetiminde bulundu, o gün yalnız **yorum**
  düzeltildi ve marka kararı kullanıcıya bırakıldı.
  **Kullanıcı 2026-08-01'de kararı verdi:** kelime `ink`, renk çerçevede. Gerekçesi —
  eşleme korunur, palete yeni token girmez, beş damganın da (dört verdict +
  TRAINING) kelimesi okunur olur,
  kaşe hissi çerçeveden gelir.

  **Sınır:** çerçevenin kendisi hâlâ WCAG 1.4.11'in metin-dışı **3:1**'ini iki
  tokende geçmiyor (saffron 2,62 · line 1,52, paper üstünde). Bu bilinçli kabul:
  durumu **kelime** taşıyor, renk pekiştirme; 1.4.11 bilgiyi metinden de veren
  öğeyi zorunlu tutmaz. Yazılıyor ki bir dahaki tur bunu keşif sanmasın.

```
┌─◠─◠─◠─◠─◠─◠─◠─◠─◠─◠─┐
│ KF ST JULIANS        │        ╭────────────╮
│ MARIA BORG           │        │ APPROVED   │  ← -8° eğik kaşe
│ IN   14:03:22        │        ╰────────────╯
│ TRUST 100  IP ✓ GPS ✓│
│ TAG 91AC-7E55 #000641│
└─◡─◡─◡─◡─◡─◡─◡─◡─◡─◡─┘
```

## Tap ekranı — kutsal alan

Çalışanın gördüğü ekran. Değiştirmeden önce sor.

- **Tek ekran, tek buton, sıfır öğrenme.** Menü, sekme, ayar yok.
- "Hello Maria" + lokasyon adı + tek büyük buton (min. 64px yükseklik,
  eldivenli/ıslak parmakla basılabilir).
- Onay ekranında **buton yok**: *"All done — you can close this page."*
  Sonraki işlem zaten yeni fiziksel dokunuş gerektiriyor.
- Başarısız işlemde "Try again" **var**.
- Marka mesajı onay sonrası gösterilir, tenant'a özel ve panelden düzenlenebilir:
  - KF check-in: *"Have a great shift — keep those kebabs rolling! 🌯"*
  - KF check-out: *"Great work today. See you next shift! 👋"*
  - KM check-in: *"Have a productive shift — stay safe on the floor! 🏭"*
  - KM check-out: *"Shift complete. Thank you for your work today! 👋"*
- Metin İngilizce (Malta pazarı). Sadeliği koru; jargon yok.

## Plaket baskısı — NFC + QR (fiziksel yüzey)

> Eklendi 2026-08-01, M5-08. **Bu bölüm bir TASARIM ÖNERİSİDİR, ölçüm değil.**
> Ölçülmüş olan tek şey aşağıda ayrıca işaretli: iki URL'nin biçimi ve uzunluğu.
> Milimetreler baskı provasında doğrulanacak; doğrulanınca bu blok "ölçüldü"
> olarak güncellenir. Tedarik/encode akışı **M8-05**, telefon envanteri **M8-07**.

Duvara monte edilen plaket **iki okuma yolu** taşır ve ikisi de birincildir:

| Yol | Ne olur | URL |
|---|---|---|
| **NFC** (NTAG 424 DNA, gömülü) | çip URL'yi her okumada yeniden yazar: `ctr`+1, taze CMAC | `…/t?tag=<uid>&ctr=<6 hane>&cmac=<16 hane>` |
| **QR** (baskı, statik) | hiçbir şey yeniden yazmaz | `…/t?tag=<uid>` |

**✅ Ölçüldü (2026-08-01):** QR URL'i, NFC URL'inin **`&ctr=`/`&cmac=` olmadan
aynısıdır** — `internal/sun.Parse` bu iki alanın **ikisinin de yokluğunu**
`Channel=qr` olarak okur, birinin tek başına yokluğunu **bozuk URL** sayar
(`params.go`). Örnek uzunluklar (`https://time.tappa.mt` tabanıyla):
NFC **75** karakter, QR **42** karakter. QR yolu uçtan uca test altında:
`internal/handler/qr_db_test.go`.

**Neden plakette QR da var — ve neden "yedek" demiyoruz.** iPhone X ve öncesi
arka planda NFC etiketi okuyamaz; o telefonlardaki çalışan için NFC URL'i **hiç
açılmaz**, yani QR onun **her günkü** yoludur. Baskı, dil ve yerleşim bunu
yansıtmalı: QR küçük bir "sorun giderme" ikonu gibi köşeye sıkıştırılmaz.

**Yerleşim (öneri).**
- Plaket dikey bölünür: **üst ~%60 dokunma alanı**, **alt ~%40 QR alanı**.
  Dokunma alanının ortasında NFC anteninin merkezi ve `tappa` kelime markası;
  telefon oraya dayanır, o yüzden orada başka bilgi yok.
- **QR kenar uzunluğu ≥ 30 mm.** Gerekçe: yaygın baskı kuralı, tarama mesafesinin
  **1/10'u** kadar kenar; duvar plaketi 20–40 cm'den taranır → 30–40 mm.
  Altına inme; eldivenli el telefonu yakınlaştırmaz.
- **Sessiz alan (quiet zone) 4 modül**, QR'ın etrafında `paper` zemin. Perforasyon
  motifi ya da çerçeve **sessiz alana giremez** — adisyon hissi için QR'ın *dışına*
  konur.
- QR **`ink` on `paper`** basılır. `tappa-green` üstüne beyaz QR **basma**:
  tarayıcılar koyu-modül/açık-zemin bekler ve ters kontrast okuma oranını düşürür.
- QR'ın altında **tek satır**, mono, uppercase, küçük punto: plaketin kendi
  **UID'sinin son 4 hanesi** (ör. `PLAQUE 000A`). Müdür "hangi kapı" derken
  telefonda değil duvarda okuyabilsin; UID sır değildir (adres çubuğunda zaten
  görünür, §4.7 kapsamında değil).

**Metin (İngilizce, ses tonu kuralına uyar).**
- Dokunma alanı: **`Hold your phone here`**
- QR alanı: **`Or scan — same thing`**
  Gerekçe: "or" iki eşit yolu ayırır, "if… doesn't work" bir başarısızlık
  anlatır. *"backup"*, *"fallback"*, *"if your phone can't"* **yazma** — M5-08
  kartının kriteri bu.
- Plaketin üstünde marka: `tappa` + `punchless` (uygulama içindeki kabuğun aynısı).

**Yapma.** QR'ı logonun içine gömme (sessiz alanı ve hata düzeltmesini yer) ·
gradient/renkli QR · yuvarlatılmış modül · plakete talimat paragrafı (üç satırdan
uzun metin duvarda okunmaz) · QR'ı NFC alanının üstüne bindirme (telefon anteni
QR'ı kapatır ve kamera odaklanamaz).

🔴 **BASKI GERÇEKLEŞTİĞİNDE EKRAN METNİ YENİDEN ELE ALINMALI (M8 devri).**
Bugün üründe **hiçbir ekran QR'dan bahsetmiyor** (ölçüldü: tap + aktivasyon
ailesinin tamamı taranıp 0 eşleşme —
`internal/handler/qr_db_test.go → TestQRScreens_SayNothingAboutTheQRRoute`) ve
bu **bilinçli**: plaketler henüz QR ile basılmadığı için ekranın var olmayan bir
şeyi tarif etmesi yanlış olurdu (kullanıcı kararı, 2026-08-01).
**Ama ölçülen kusur duruyor:** tur slayt 1 *"If your phone does not react, ask
your manager."* diyor; **iPhone X ve öncesi arka planda NFC etiketi okuyamaz**,
yani o çalışan için sayfa **hiç açılmaz** ve bu cümle onun **her günkü yolunu bir
arıza gibi** çerçeveliyor. Plaketler QR ile basıldığı anda slayt 1 (ve genel
olarak iPhone X yolu) *"Or scan the code on the plaque — it works the same way"*
yönünde yeniden yazılmalı. O değişiklik §9 kapsamındadır (önce sor) ve
`result_test.go`'nun **üç beyaz listesi** + tur testleri + yukarıdaki tripwire
testi **birlikte** güncellenir.

**Açık soru (M8-05'e).** QR'a `?src=qr` gibi bir işaret **KOYULMADI ve
koyulmamalı**: kanal, `ctr`/`cmac`'in yokluğundan **sunucuda** türetiliyor
(state.md N2) ve URL'e istemcinin taşıdığı bir kanal işareti eklemek o türetimi
ikinci bir kaynakla yarıştırırdı. Baskı sağlayıcısı "izleme parametresi ekleyelim
mi" diye sorarsa cevap **hayır**.

## Ses tonu

Kısa, sıcak, kendinden emin. *"Tapped in at 14:03."* — *"Your check-in operation
has been successfully processed."* değil. Hata mesajı suçlamaz, ne yapılacağını
söyler: *"That tag was replaced. Ask your manager for the new plaque."*

Slogan: **No app. No device. No fingerprints. Just tap.** · Kampanya: *Go punchless.*

## templ + Tailwind kuralları

- Bileşenler `web/templates/components/`, sayfalar `web/templates/pages/`.
- Tekrar eden desen (docket, damga, buton, boş durum) **bileşendir** — kopyala-yapıştır
  Tailwind zinciri değil.
- Semantik yardımcı sınıflar `input.css` içinde `@layer components` altında
  (`.docket`, `.stamp`, `.stamp--approved`).
- 🔴 **Tailwind `.templ` dosyasını ham metin olarak tarar — YORUMLAR VE ÖZNİTELİK
  DEĞERLERİ DÂHİL. Ve bu tuzak "dikkat edilecek bir şey" değil, ZATEN ATEŞLENMİŞ.**
  Ölçüldü (2026-08-01, HEAD `b86bc5c` kaynaklarından izole dizinde derlenerek):
  `app.css`'te hiçbir `class` özniteliğinde geçmeyen **7 çıplak `.sınıf` kuralı**
  var — **334 bayt**, 14.256 baytlık dosyanın **%2,3'ü**:

  | Ölü kural | Nereden doğdu |
  |---|---|
  | `.filter` (185 B) | `result.templ` — *"NO verdict **filter**"* |
  | `.visible` (28 B) | `result.templ` — *"a **visible** edit"* |
  | `.relative` (28 B) | `activate.templ` — *"**relative** paths"* |
  | `.min-h-16` (26 B) | `tap.templ` — *"gives the target **min-h-16** = 64px"* |
  | `.static` (24 B) | `/**static**/css/app.css` URL'leri + düzyazı |
  | `.fixed` (22 B) | *"the **fixed** status vocabulary"* (3 dosyada) |
  | `.hidden` (21 B) | `<input type="**hidden**">` öznitelik değerleri + düzyazı |

  `.relative` ve `.min-h-16` gerçekten kullanılıyor **ama `@apply` ile** — ölçüldü:
  `@apply` bildirimleri bileşen kuralının **içine gömüyor** (`.docket` kendi
  `position:relative`'ini, `.tap-button` kendi `min-height:4rem`'ini taşıyor), yani
  çıplak kurala ihtiyaç yok; o da düzyazıdan doğmuş.

  Aynı mekanizma **ters yönde de** çalışıyor: bir sınıf adı yalnızca bir **yorumda**
  geçtiği için derlenmeye devam edebilir. Ölçüldü — `result.templ`'den iki damga
  sınıfı **markup'tan** silindiğinde beş modifier'ın **beşi de** `app.css`'te kaldı
  (`stamp--approved` bu dosyada 2 kez geçiyor: 1 `class` özniteliği + 1 yorum).

  **Kural:** yorumda utility'yi **tarif et, yazma**. Bu görevde iki isim yazmak taze
  build'e **+330 bayt** ve iki kural ekledi; yeniden yazılıp sıfırlandı. **Mevcut 7
  ölü kural bu görevde TEMİZLENMEDİ** — kapsam dışı, ve `.hidden`/`.static` gibi
  isimler ileride gerçekten gerekebilir. Sayıldı ve yazıldı, o kadar.
- Etkileşim HTMX ile: `hx-post`, `hx-target`, `hx-swap`. İstemci state'i
  gerektiren bir şey istiyorsan önce dur ve gerçekten gerekli mi sor.
- Erişilebilirlik: durum **asla** yalnız renkle anlatılmaz — damga metni de var.
  **Kontrast AA — ve bu kural ölçülmeden yazıldığı için 2026-08-01'e kadar bizzat
  damgalarda çiğneniyordu** (yukarıdaki tablo). Yeni bir renkli metin yazarken
  **hesapla**: zemin `paper`/`porcelain` hangisi, alfa varsa sRGB'de harmanla,
  normal metin **4,5:1**. `text-<token>` yazmak "AA" demek değildir.
  Dokunma hedefi ≥ 44px. `prefers-reduced-motion` saygılı.
- Karanlık tema **yok** (şimdilik) — plaket ortamı aydınlık, yarım iş yapma.

## Yapma

Emoji ikon seti (marka mesajları hariç) · yuvarlak hap butonlar · gradient ·
glassmorphism · sallanan animasyon · stok illüstrasyon · birden çok vurgu rengi ·
mono olmayan sayı · adisyon yerine düz tablo satırı.

## Tenant slotları (taslak)

> Eklendi 2026-10-02, M10 WL-0. **Taslak:** kod henüz yok (WL-1…WL-9 yazacak); kesinleşince
> WL-12 bu satırı günceller. Normatif kaynak:
> [ADR 0023](../../../docs/adr/0023-tenant-markasi-ve-arayuz-kurali.md) (slotlar, kontrast,
> §9 onayı) ve [ADR 0024](../../../docs/adr/0024-kullanici-yukledigi-gorsel.md) (logo dosyası).
> Bu bölüm yukarıdaki metni değiştirmez; iki kurala **sayılı istisna** getirir: *"Palet
> dışına çıkma"* (tenant accent'i aşağıdaki dolgularda; logonun pikselleri aşağıdaki dört
> yerde — bir görselin renkleri palet kuralının konusu değil) ve *"birden çok vurgu rengi"*
> (panel şeridi ve Account önizlemesinin tap düğmesi — "Dokunulmaz" altında). Taptime'ın
> kendi arayüzü için palet kuralı aynen geçerli.

Tenant iki şey verir: **bir accent** (altı haneli hex) ve **bir logo** (PNG/JPEG, sunucuda
yeniden kodlanmış, uzun kenarı ≤512 px). Yazı tipi, ikinci renk, metin, favicon yok.

### Hangi slot nerede

| Yüzey | Logo | Accent | Not |
|---|---|---|---|
| Tap ekranı | üstte logo, **altında** "taptime · punchless" (K-2b) | **tap düğmesinin zemini** | kullanıcı kararları D-C (2026-09-24), K-2a ve K-2b (2026-10-02); hâlâ tek düğme, *"Tap"*, ≥64 px. Logo yok ama accent varsa başlıktaki `taptime` **ink** (K-2a) |
| Sonuç ekranı | üstte logo, altında co-brand (K-2b) | **yok** — renkler durumu anlatır | D-C; logo yoksa bugünkü Wordmark |
| Panel kabuğu | başlık + tenant adı | **4 px şerit** | birincil düğmeler yeşil kalır (K4) |
| Panel → Account → "Your brand" önizlemesi | önizlemedeki tap başlığında (kaynak `/admin/brand/logo/{sha}`) | önizlemedeki **tap düğmesi** — **kaydedilmiş** accent | tap ekranının bileşenleri (bölmeyi WL-9 yapar), **gönderilemez**: `<form`/`/api/checkin` 0, düğme `submit` değil, bir formun içinde değil, `form=` yok — WL-7. Aday hex önizlemeye gitmez; `<input type="color">`'un kendi rengiyle görünür |
| Aktivasyon, tur, problem, giriş, landing, legal, signup, operatör | yok | yok | Taptime (aktivasyon: K6; tur: faz 2'de logo — karar verilmedi) |
| E-posta | faz 1 yok | yok | yalnız tenant adı, gövdede (K8) |

Plaket başka bir tenant'ınsa tap ve sonuç ekranı Taptime varsayılanıyla açılır.
`.tap-button` sınıfı yedi yerde kullanılıyor ama accent'i sınıf değil **sayfanın tema
bağlantısı** taşır; tema tap ekranına ve panele bağlanır (panelin içindeki Account önizlemesi
de bu yüzden accent'li düğme gösterir — bilerek), diğer altı kullanım yeşil kalır.

### Accent'in kontrast kuralı

Accent bir **dolgudur** — tap düğmesinin zemini (tap ekranı, Account önizlemesi) ya da şerit. Metin rengi, ince çizgi, ikon rengi
olarak kullanılmaz: kapı accent'in kendisinin bir zeminde okunurluğunu ölçmez (ör. sarı
`#FFC72C` porcelain'de 1,36:1). Derlenmiş CSS'te `--brand-accent` yalnız `background-color`
içinde geçer; `--brand-on-accent` yalnız `color`'da, `--brand-edge` yalnız kenarda (WL-5'in testi
özelliğe bakar, seçiciye değil).

| Zemin | Üstündeki | Eşik | Kural |
|---|---|---|---|
| accent (tap düğmesi) | düğme metni — paper ya da ink, kontrastı yüksek olan | **4,5:1** | metin 20 px / 400, "büyük metin" değil; ikisi de 4,5'e ulaşmıyorsa renk **reddedilir**, form aynı tonun koyulaştırılmış hâlini önerir |
| porcelain (sayfa) | accent'li düğmenin sınırı | **3:1** (WCAG 1.4.11) | ulaşmıyorsa düğmeye **2 px ink kenar** (ink on porcelain 14,32:1); kenar düğmenin kutusunu değiştirmez |
| porcelain (panel) | 4 px şerit | — | dekoratif, `aria-hidden`; şart yok |

Sınırlar palet değerlerinden **hesaplanır**, elle yazılmaz (Go'daki palet kopyası
`tailwind.config.js`'e bir eşitlik testiyle bağlı): paper metin `L ≤ 0,1789826956…`, ink metin
`L ≥ 0,2368152180…`, aradaki bant reddedilir; kenar `L > 0,2542173…`. Yazılı değerler kesiktir:
dört haneli 0,1790 / 0,2368 1 092 rengi, altı haneli 0,178983 / 0,236815 bile 14 rengi 4,5'e
ulaşmadan geçirir — testte literal kullanma, formülü kullan.

| Renk | Sonuç |
|---|---|
| `#DA291C` | paper metin, 4,78:1 |
| `#FFC72C` | ink metin 10,56:1 + ink kenar |
| `#808080` | red (en iyisi 4,17:1) |
| tappa-green | paper metin, 7,73:1 — marka ayarlamamış tenant'ın varsayılanı |

### Logo: yerleşim ve boyut (öneri — WL-8/WL-9 ölçer)

- **Tap ve sonuç ekranı:** Wordmark'ın yerinde, **sabit yükseklikli logo yuvası** ve
  **altında** co-brand satırı (K-2b). Ölçüldü: bugünkü başlık **28 px**, co-brand satırı tek
  başına **15 px**. Yuva `H`, aralık `g` ise düğme `H + g − 13` kayar; WL-9'un **16 px**
  bütçesiyle `H + g ≤ 29` → ör. 4 px aralıkla yuva **≤25 px**. 40 px'lik yuva 31 px kaydırır
  — bu durumda WL-9'un bütçesi değişmeli; sayıyı orkestratör WL-9'da koyar. Genişlik oranla,
  öneri en çok sütunun yarısı. `width`/`height` öznitelikleri saklanan boyuttan hesaplanıp
  yazılır — yüklenirken sayfa kaymaz.
- **Panel:** başlıkta logo (öneri 32 px yükseklik) + tenant adı (Space Grotesk, kalın) +
  co-brand satırı; 4 px şerit kabuğun en üstünde, sütun genişliğinde.
- **Zemin:** logo sayfa zemininde (porcelain) durur; çerçeve, gölge, yuvarlatma yok. Açık
  renkli logo uyarısı **porcelain'e** karşı hesaplanır (logo orada oturuyor).
- **`alt`** = tenant adı. Tap ekranındaki yeni metin budur; co-brand satırı bugün Wordmark'ta
  duran iki kelimeyi taşır.
- Logo `<img>` ile gelir — CSS arka plan görseli değil (`alt` taşımaz, aynı CSP iznini ister).

### Co-brand

Logo olan yüzeyde küçük bir satır kalır: **"taptime · punchless"** — bugünkü `punchless`
tonunda (10 px mono, büyük harf, geniş aralık, ink/70; porcelain'de 5,70:1). Tap ve sonuç
ekranında yeri **logonun altı** (kullanıcı kararı K-2b, 2026-10-02). Gerekçe: aktivasyonun
GDPR cümlesi Taptime'ı işleyen olarak adlandırıyor; çalışanın her gün gördüğü ekran aynı
işleyeni göstermeli.

**Logo yokken:** accent varsa tap ekranının başlığındaki `taptime` **ink** olur (K-2a,
2026-10-02; porcelain'de 14,32:1) — ekranda tek vurgu rengi tenant'ın düğmesi kalır. Accent de
yoksa başlık bugünkü gibidir.

### Dokunulmaz — tenant markasından bağımsız

Beş kaşe damgası ve durum→renk eşlemesi (kelime ink, renk çerçevede) · tomato = hata/yıkıcı ·
saffron = FLAGGED/geç · `Notice` · docket + perforasyon · `.docket-label` · panelin birincil ve
yıkıcı düğmeleri, sekme vurgusu, odak halkaları · sayfa zeminleri ve metin tonları · yazı
tipleri · sonuç ekranında accent yok (logolu tenant'ta yalnız Wordmark yerini logo + co-brand
satırına bırakır) · onay kutularının `accent-color`'ı (adı "accent" ama
tenant accent'i **değil**).

Panelde iki renk yan yana durur — yeşil panelin eylemlerinde, tenant rengi şeritte ve Account
önizlemesinin tap düğmesinde. Bu, yukarıdaki *"birden çok vurgu rengi"* yasağının **bilinçli**
istisnasıdır (K4): şerit eylem ya da bilgi taşımaz; önizlemedeki düğme bir eylem değil, tap
ekranının görüntüsüdür.

### Uygularken

- Yeni token'lar `brand`, `on-brand`, `brand-edge` — şablon **yorumunda** sınıf adı yazma,
  tarif et (yukarıdaki Tailwind tuzağı aynen geçerli).
- Marka okunamadıysa sayfa düşmez: varsayılana döner (§4.6). Marka yoksa HTML bugünküyle
  bayt-aynı: ek `<link>`, `<img>`, `img-src` yok.
- Accent'i sonuç ekranına, damgaya, docket'e ya da birincil düğmeye taşıma.
- Logo için uzak URL kullanma — e-postada da (izleme pikseli).
