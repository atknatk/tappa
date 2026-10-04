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
- Marka mesajı onay sonrası gösterilir (yalnız sayılan `ok`'ta; practice, flag, ignored ve
  reject'te yok). **İşletmenin türüne göre seçilir, tenant düzenleyemez** — Account ekranı
  bunu söyler (`TestAccount_SaysTheMessagesCannotBeEdited`); white-label bunu değiştirmedi
  (ADR 0023 §9 madde 6). *(2026-10-06, M10 WL-12 düzeltmesi: burada "tenant'a özel ve
  panelden düzenlenebilir" yazıyordu; ürün öyle değil — `result.templ` `brandMessage`.)*
  - restoran, giriş (KF): *"Have a great shift — keep those kebabs rolling! 🌯"*
  - restoran, çıkış (KF): *"Great work today. See you next shift! 👋"*
  - üretim, giriş (KM): *"Have a productive shift — stay safe on the floor! 🏭"*
  - üretim, çıkış (KM): *"Shift complete. Thank you for your work today! 👋"*
  - diğer türler: *"Have a great shift!"* / *"Thanks for today. See you next time! 👋"*
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
> **ADR 0025 (2026-10-04):** aşağıda anılan tur **kaldırıldı**; aktivasyon sihirbazı o
> cümleyi taşımıyor ve QR'dan bahsetmiyor (tripwire sihirbazın dört adımını da
> tarıyor). Yerine gelen daha sert kusur: **QR ile aktivasyon YOK** — aktivasyon
> fiziksel NFC dokunuşu ister, yani iPhone X ve öncesi bugün aktive olamaz (ürün
> kararı bekliyor). Aşağıdaki paragraf tarihseldir.

**Ama ölçülen kusur duruyordu:** tur slayt 1 *"If your phone does not react, ask
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

## Tenant slotları

> Eklendi 2026-10-02 (M10 WL-0) — o gün kod yoktu ve yerleşim bir öneriydi; **2026-10-06'da
> WL-12 ölçülen kurallarla kesinleştirdi** — WL-1…WL-9 sevk edildi (HEAD `26e9ce0`). Normatif kaynak:
> [ADR 0023](../../../docs/adr/0023-tenant-markasi-ve-arayuz-kurali.md) (slotlar, kontrast,
> §9 onayı) ve [ADR 0024](../../../docs/adr/0024-kullanici-yukledigi-gorsel.md) (logo dosyası).
> Aşağıdaki her sayı ya bir ADR ölçüm notundan ya da bir testten gelir ve kaynağı yanında
> yazılıdır; çelişirse ADR geçerlidir. Chrome ölçümleri **bir kez** alındı (Chrome 154), pin
> değildir; pin olanlar test adıyla anılır.
> Bu bölüm yukarıdaki metni değiştirmez; iki kurala **sayılı istisna** getirir: *"Palet
> dışına çıkma"* (tenant accent'i aşağıdaki dolgularda; logonun pikselleri aşağıdaki dört
> yerde — bir görselin renkleri palet kuralının konusu değil) ve *"birden çok vurgu rengi"*
> (panel şeridi ve Account önizlemesinin tap düğmesi — "Dokunulmaz" altında). Taptime'ın
> kendi arayüzü için palet kuralı aynen geçerli.
>
> **Tailwind bu dosyayı taramaz** — `tailwind.config.js`'in `content`'i yalnız
> `web/templates/**/*.templ` ve `web/static/js/**/*.js`. Ölçüldü (2026-10-06, WL-12): bu
> bölüm yazıldıktan sonra derlenen `app.css` öncekiyle bayt-aynı (50 992 B, sha256 aynı);
> derlemede bulunmayan iki yardımcı sınıf adı bu dosyaya yazılınca da bayt-aynı kaldı, aynı iki
> ad `tap.templ`'de bir yorum satırına yazılınca `app.css` 51 255 B'a çıktı (kontrol: tarama
> farkı görebiliyor). Aşağıdaki sınıf adları bu yüzden kural doğurmaz; aynılarını
> şablon **yorumuna** yazma (yukarıdaki *"templ + Tailwind kuralları"* tuzağı).

Tenant iki şey verir: **bir accent** (altı haneli hex) ve **bir logo** (PNG ya da JPEG,
sunucuda yeniden kodlanmış, uzun kenarı ≤ 512 px — ADR 0024 §1–§3). Yazı tipi, ikinci renk,
metin, favicon yok (ADR 0023 §1, §9). İkisini yalnız işletmenin **aktif sahibi** değiştirir:
handler'da `mayEditAccount`, domain'de ikinci kez, rol ve durum DB'den
(`TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite`).

### Hangi slot nerede (ADR 0023 §2 — sayılı liste)

| Yüzey | Logo | Accent | Logonun `alt`'ı | Kaynak |
|---|---|---|---|---|
| Tap ekranı | başlıkta 24 px yuva, altında 4 px aralıkla *"taptime · punchless"* (K-2b) | **tap düğmesinin zemini** | işletme adı | WL-9 — `TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse` |
| Sonuç ekranı | tap ekranınınkiyle aynı başlık | **yok** — tema `<link>`'i 0, renkler durumu anlatır | işletme adı | WL-9 — `TestResultScreen_DrawsTheLogoAndNoAccent` |
| Panel kabuğu | başlıkta 32 px kutu + işletme adı + co-brand | **4 px şerit** | **boş** (`alt=""`) — ad logonun yanında görünür metin | WL-8 — `TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection` |
| Account → *"Your brand"* önizlemesi | önizlemedeki tap başlığı, kaynak `/admin/brand/logo/{sha}` | önizlemedeki tap düğmesi — **yalnız kaydedilen** accent | işletme adı | WL-7 — `TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit` |
| Aktivasyon, tur, problem, giriş, AdminChoose, parola sıfırlama, landing, legal, signup, operatör | yok | yok | — | Taptime (aktivasyon: K6; tur ve practice: faz 2, *karar verilmedi*) |
| E-posta | faz 1'de yok | yok | — | K8: markadan en çok işletme adı, gövdede; adın gösterilip gösterilmeyeceği EM-7'nin bekleyen kullanıcı kararı (WL-11 SES akışıyla bekliyor) |

- **Başka işletmenin plaketi:** tap ve sonuç ekranı Taptime varsayılanıyla açılır — `<img>`,
  tema bağlantısı ve `/t/logo/` 0 (`TestTapPage_AnotherBusinesssPlaqueShowsNoBrand`,
  `TestResultScreen_NoBrandWhereThePlaqueIsNotThisBusinesss`).
- **Marka yoksa bayt-aynı:** tap, sonuç ve aktivasyon ailesinin 33 render'ı
  (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`) ve WL-8'in 24 bileşen render'ı
  (`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` — 16'sı panel kabuğu, 8'i
  `documentHead`'e ulaşan öteki kabuklar: `Page`, `PageWithScript`, `Marketing` ve
  `MarketingWithScript`, `Auth` iki hâliyle, `Operator` ve `OperatorWithScript`) WL öncesiyle
  bayt bayt aynı — ek `<link>`, `<img>`, `img-src` yok.
- **Marka okunamazsa sayfa düşmez (§4.6):** okunamayan marka (DB hatası, yarım logo satırı,
  kanonik olmayan accent) bütünüyle Taptime'a döner ve ERROR yazar; kapının **bugün**
  reddettiği kayıtlı accent (palet ya da eşik sonradan değişti) yalnız accent'i düşürür, logo
  kalır, WARN (ADR 0023 §7 WL-9 notu karar 4, WL-8 notu karar 7;
  `TestTapPage_ABrandReadFailureRendersTaptimesPage`,
  `TestTapPage_AnAccentTheGateRefusesTodayKeepsTheLogo`).
- **Tap düğmesinin sınıfı sekiz `class` özniteliğinde** (2026-10-06'da sayıldı: `tap.templ`'de
  iki — gönderen yüz ve önizleme yüzü —, aktivasyon ailesinde altı; WL-0'da yediydi, önizleme
  yüzü WL-9'un bölmesiyle geldi), ama accent'i sınıf değil **sayfanın tema bağlantısı** taşır:
  tema yalnız tap ekranına ve panel kabuğuna, `app.css`'ten hemen sonra bağlanır; aktivasyon,
  tur, problem ve onay ekranlarındaki altı kullanım yeşil kalır (ADR 0023 §2, §4). Panelin
  içindeki Account önizlemesinin düğmesi accent'i bu yüzden **bilerek** alır.

### Tap ve sonuç ekranının başlığı — üç şekil (ADR 0023 §5–§7, WL-9 notu)

| İşletmede | Başlık | Ölçülen (CDP, 390×844, bir kez) |
|---|---|---|
| logo var | 24 px yuvada logo, altında 4 px aralıkla *"taptime · punchless"* (10 px mono, büyük harf, ink/70) — K-2b, tap ve sonuç ekranı | başlık 28 → **43 px**, tap düğmesi **15 px** aşağı (üst kenarı 214 → 229), layout-shift **0**; co-brand porcelain'de **5,70:1** |
| logo yok, accent var | Wordmark'ın kutusu, `taptime` **ink** — K-2a, yalnız tap ekranı (sonuçta accent olmadığı için bugünkü Wordmark) | kayma 0; ink `taptime` porcelain'de **14,32:1** |
| ikisi de yok | bugünkü Wordmark, bayt bayt (yeşil `taptime`, 6,85:1) | — |

- **Yuvanın aritmetiği:** logo varken başlık `H + g + 15` (yuva `H`, aralık `g`, co-brand satırı
  15 px), düğme `H + g − 13` kayar; WL-9'un 16 px bütçesi `H + g ≤ 29` ister. Orkestratörün WL-9
  kararı **H = 24, g = 4 → 15 px** (ADR 0023 §5, *Karar verilmedi* kapandı).
- **Logo kutusu:** yükseklik yuvanın 24 px'i; genişlik saklanan orandan, en çok sütunun yarısı
  (`max-w-[50%]`); `width`/`height` öznitelikleri **saklanan kutudur**, baytlar gelmeden yer
  ayrılır (kontrol: öznitelik ve yuva yokken bir kayma, 0,0144). Ölçülen: 512×128 → 96×24 ·
  512×16 → 179×24 · uzun ince 128×512 → **6×24** — yuva kararının bedeli (WL-9 sınır 7;
  `TestTapPage_TheLogoCarriesItsStoredBox`).
- Ekran hâlâ **tek düğme**, kelimesi *"Tap"*, en az 64 px; tap ekranındaki tek yeni metin
  logonun `alt`'ıdır (ADR 0023 §6 madde 10).
- **Kırık logo:** logo isteği 404 alırsa (ör. logo değişti, eski sayfa açık) tarayıcı 24 px
  yuvada kırık görsel simgesini ve `alt`'ı — işletme adını — kırpılmış gösterir. `alt` = ad
  kabul şartının bedeli, kod değişmez (WL-9 sınır 11); ad serbest metin olduğu için bu,
  [ADR 0005](../../../docs/adr/0005-kabul-edilen-riskler.md) risk 9'un (marka taklidi) bir
  biçimidir.
- Logo porcelain zeminde durur: çerçeve, gölge, yuvarlatma yok. `<img>` ile gelir — CSS arka
  plan görseli değil (`alt` taşımaz, aynı CSP iznini ister).

### Panel kabuğu (ADR 0023 WL-8 notu)

- *Markalı* = kapıdan **bugün** geçen accent **ya da** logo (karar 3). Markalıda Wordmark'ın
  yerine: 4 px şerit (yalnız accent geçiyorsa; başlığın ilk öğesi, `aria-hidden`) · logo
  (varsa) · işletme adı (Space Grotesk kalın; renk sınıfı yok, sayfanın ink'i — porcelain'de
  14,32:1) · altında co-brand (5,70:1). **Yalnız accent'li** işletmede de ad ve co-brand
  çizilir, yeşil `taptime` kalkar. Ad ve co-brand paragrafları tam sınıf listeleriyle pinli.
- **Logo kutusu: 32 px yükseklik, en çok 192 px genişlik**, oran korunur, saklanandan büyük
  çizilmez — küçük logo büyütülüp bulanıklaşmaz; 512×128 → 128×32
  (`TestPanelLogoOf_KeepsTheProportionsInsideTheSlot`, 1–512 × 1–512 taraması; karar 6).
- **`alt=""`** (karar 9): ada eşit bir `alt` ekran okuyucuda adı iki kez okutur ve kırık logoda
  adı kutunun içine basıp sayfayı iter — 114 karakterlik adla kutu 128×216, içerik +145 px
  (ölçüldü). Boş `alt` ile kırık logo kutunun dışına **0 px** basar (sınır 5).
- Şerit 1024 genişlikte sütun boyunca 992×4 px; tenant rengi yalnız orada. Birincil düğme ve
  sekme işareti yeşil kalır (K4).

### Account → *"Your brand"*: önizleme ve editör (ADR 0023 WL-7 notu, ADR 0024 WL-7 notu)

- **Önizleme tap ekranının kendi üç bileşenidir**, kopyası değil: `layout.BrandHeader`,
  `pages.TapHeading` (oturumdaki yöneticinin adıyla, mekânsız) ve
  `pages.TapButtonFace(TapButtonPreview)`; üçü blokta **tam birer kez**.
- **Gönderilemez:** blok sayfanın **tek** `inert` öğesidir; içinde `<form` 0 ve `/api/checkin`
  0, sayfada `form=` özniteliği 0; düğme `type="button"` ve hiçbir formun soyundan değil
  (`TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit`). `inert` desteklemeyen bir
  tarayıcıda düğme odaklanabilir, yine gönderemez (WL-7 sınır 13).
- **Yalnız kaydedilen accent:** önizleme kabuğun tek tema bağlantısını okur; aday renk
  önizlemeye gitmez, yalnız `<input type="color">`'un tarayıcının çizdiği kendi boyasında
  görünür. Reddedilen bir kayıttan sonra önizleme ve editör dışındaki sayfa düz yüklemeyle
  bayt-eşittir (`TestBrandPreview_ShowsOnlyTheSavedAccent`,
  `TestBrandPreview_ARefusedColourLeavesThePreviewAsSaved`). Gerekçe: aday için ikinci bir
  tema `<link>`'i `:root`'u yeniden yazar ve şeridi de boyardı (ADR 0023 §2).
- **Logo** panel rotasından (`/admin/brand/logo/{sha}` — panel çerezi tap yüzeyine gitmez),
  saklanan kutuyla, `alt` = ad (`TestBrandPreview_TheLogoIsThePanelRoute`). Önizleme kabuğun
  kendi marka okumasından çizilir; ayrı bir okuma sayfanın `img-src`'siyle ayrışırdı (WL-8
  devri, WL-7 karar 2).
- **İki accent girdisi, betik yok:** renk seçici + kod kutusu; yazılan kod seçiciye üstün gelir
  (yardım metni söyler). Okunaksız renkte form, denenen renkle ve `Suggest`'in rengini taşıyan
  **ayrı** bir formla geri gelir; örnek kutusu devre dışı bir `<input type="color">` — CSP satır
  içi stile izin vermez (`TestBrandAccent_AnIllegibleColourComesBackWithItsSuggestion`).
  Ölçülen: `808080` → öneri `#757575`.
- **Sıfırlama alan başınadır** (logo ya da accent, her biri tek iz satırı); yönetici (manager)
  önizlemeyi görür, formu görmez (`TestBrandEditor_AManagerSeesThePreviewAndNoForm`).
- **Dokunma hedefleri ≥ 44 px** (`TestBrandEditor_EveryControlIsATouchTarget`; CDP, 390 px:
  dosya girdisi 324×44, renk seçici 96×44, kod kutusu 220×44, öneri örneği 44×44, düğmeler
  117–178 × 44–48, önizleme düğmesi 290×64).
- Yükleme yardımı: *"A PNG or JPEG file, up to 512 KB: a logo, not a photo of a person."*
  (ADR 0024 §7 — yüz tespiti yok, §4.1). Küçültülmüş çıktı 256 KiB'ı aşarsa ret cümlesi
  JPEG'i önerir (fotoğraf benzeri 512 px PNG 440 KiB, aynı görüntü JPEG'te 43 KiB — ADR 0024
  S19).

### Accent'in kontrast kuralı — kapı (ADR 0023 §3)

Accent bir **dolgudur** — tap düğmesinin zemini (tap ekranı, Account önizlemesi) ya da şerit.
Metin rengi, ince çizgi, ikon rengi olarak kullanılmaz: kapı accent'in kendisinin bir zeminde
okunurluğunu ölçmez (ör. sarı `#FFC72C` porcelain'de 1,36:1).

| Zemin | Üstündeki | Eşik | Kural |
|---|---|---|---|
| accent (tap düğmesi) | düğme metni — paper ya da ink, kontrastı yüksek olan (`OnColor`) | **4,5:1** | metin 20 px / 400, "büyük metin" değil; ikisi de 4,5'e ulaşmıyorsa renk **reddedilir** (`Check`), form aynı tonun koyulaştırılmış hâlini önerir (`Suggest`) |
| porcelain (sayfa) | accent'li düğmenin sınırı | **3:1** (WCAG 1.4.11) | ulaşmıyorsa düğmeye **2 px ink iç gölge** (`Edge`; ink porcelain'de 14,32:1); kutu değişmez — ölçülen temaların hepsinde 358×64, `border` 0 |
| porcelain (panel) | 4 px şerit | — | dekoratif, `aria-hidden`; şart yok |

- **Kapı 24 bitlik renklerin 1 949 736'sını (%11,62) reddeder.** Sınırlar palet değerlerinden
  **hesaplanır**: paper metin `L ≤ 0,1789826956…`, ink metin `L ≥ 0,2368152180…`, aradaki bant
  red; kenar `L > 0,2542173…`. Yazılı değerler kesiktir — dört haneli 0,1790 / 0,2368
  **1 092** rengi, altı haneli 0,178983 / 0,236815 bile **14** rengi 4,5'e ulaşmadan geçirir:
  testte literal kullanma, formülü kullan. Pinler:
  `TestAccent_TheADRPrintsTheBoundariesThePaletteYields`,
  `TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`; Go'daki palet kopyası
  `TestPalette_TheGoCopyEqualsTailwindConfig` ile `tailwind.config.js`'e bağlı.
- **`Suggest` koyulaştırır** (orkestratör kararı, 2026-10-03 — açma yönü düğme metnini paper'dan
  ink'e çevirirdi): HSL'de ton ve doygunluk sabit, açıklık iner; o yolda kapıdan geçen en
  parlak renk, geçen renk değişmeden döner (`TestSuggest_ThePathIsTextbookHSL`,
  `TestSuggest_IsTheBrightestPassingColourOnTheExactPath`).
- **Aynı kapı her tarafta:** editör (form yeniden çizilir, yazma yok), domain
  (`ErrAccentIllegible`), tema rotası (`GET /brand/theme/{HEX}.css` yalnız kanonik **ve**
  kapıdan geçen hex'e 200 — `TestBrandTheme_AnswersOnlyACanonicalLegibleHex`) ve sayfaların
  okuma tarafı (bugün reddedilen kayıtlı accent çizilmez — yukarıda).

| Renk | Sonuç (ADR 0023 §3 tablosu; `TestAccent_TheDesignTableHolds`) |
|---|---|
| tappa-green `#1F5C41` | paper metin 7,73:1 — marka ayarlamamış tenant'ın varsayılanı |
| `#DA291C` | paper metin 4,78:1 |
| tomato `#BE3D2A` | paper metin 5,30:1 — geçer; durum renklerine yakınlık kuralı yok (ADR 0023 sınır 2, *karar verilmedi*) |
| `#FFC72C` | ink metin 10,56:1 + ink kenar |
| saffron `#D98E2B` | ink metin 6,16:1 + ink kenar |
| `#808080` | **red** (en iyisi 4,17:1) → öneri `#757575` |
| `#E0457B` | **red** (en iyisi 4,16:1) |

### Açık logo uyarısı (ADR 0023 WL-7 notu, karar 7)

Logonun piksellerinin **alfa ağırlıklı ortalama bağıl parlaklığı** ile **porcelain** (logonun
oturduğu zemin) arasındaki oran **1,5:1**'in altındaysa logo **kaydedilir** ve yanıt
`logo-saved-light` uyarısıyla döner — ret değil. Ortalanan doğrusal parlaklıktır, 8 bitlik
değerler değil: siyah-beyaz yarı yarıya 1,66:1 (uyarı yok); eşiğin iki yanındaki en yakın
griler `C5C5C5` 1,5002:1 ve `C6C6C6` 1,4847:1; hiç opak pikseli olmayan logo 1:1. Yalnız
`Normalize`'ın çıktısını okur (`TestLogoLight_AlphaWeightedLuminanceOnPorcelain`,
`TestBrandUpload_ALightLogoIsSavedWithAWarning`). Uyarı yalnız yükleme yanıtındadır; sonraki
ziyaret uyarmaz (ADR 0024 WL-7 sınır 9).

### Token'lar ve derlenmiş CSS (ADR 0023 §4 WL-5 notu)

- **Token'lar `colors` altında değil, ADR'nin izin verdiği özelliğin altında:**
  `backgroundColor.brand`, `textColor['on-brand']`, `boxShadowColor['brand-edge']` → yazılan
  yardımcılar `bg-brand`, `text-on-brand`, `shadow-brand-edge`. `colors` dokuz token'lık
  paletin bloğudur ve Go kopyası onu satır satır okur; şablona yazılan `text-brand`,
  `border-brand`, `bg-on-brand`, `ring-brand-edge`, `text-brand-edge` derlenmiş CSS'te **kural
  üretmez** (ölçüldü). Accent'i metin ya da kenar rengi yapmanın yardımcı yolu yapısal olarak
  kapalı.
- **`:root` varsayılanları** tappa-green / paper / kenar yok: markasız sayfanın computed
  style'ı WL öncesiyle aynı (`TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme`; CDP'de düğme
  `rgb(31, 92, 65)` / `rgb(255, 253, 244)`, `box-shadow` `none`).
- **Slot tablosu (`themeSlots`):** `.tap-button` üç değişkenle, `.panel-stripe` yalnız
  `--brand-accent` ile. `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` derlenmiş
  `app.css`'te **hem seçiciye hem özelliğe** bakar: `--brand-accent` yalnız
  `background-color`'da, `--brand-on-accent` yalnız `color`'da, `--brand-edge` yalnız `inset`
  gölgede. Tabloda olmayan bir seçicinin marka değişkeni okuması kırmızıdır (ör. damgaya accent
  zemini, düğmenin `color`'ına accent).
- **Geçiş golden'ı:** derlenmiş `app.css`'te `brand` sözcüğünün, harf büyüklüğünden bağımsız,
  her geçişi `themeBrandGolden`'a eşit olmalı — bugün **beş yer**: `:root` varsayılanları,
  `.tap-button` ana kuralı, `.tap-button` iç gölgesi, `.panel-stripe` ve açılış sayfasının
  `.lp .plaque .p-brand`'i (`TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`). Adında `brand`
  geçen **yeni bir sınıf** bu testi kırmızıya çevirir; o düzenleme golden'ı da günceller.
- Bu üç test derlenmiş `app.css` ister: `make css` koşulmamış yerel bir koşuda **SKIP**
  ederler — SKIP bir geçiş değildir; CI önce derler.
- 🔴 **Kalıtım yolu testsizdir:** tap düğmesine bir sözde öğe (`::before`/`::after`) +
  `background: inherit` (ya da `currentColor`) eklemek accent'i değişken okumadan taşır ve hiçbir
  test görmez; ebeveynde unutulmuş `position: relative` ile accent docket'in üstüne ya da bütün
  ekrana yayılır (ölçüldü — WL-5 sınır 10). Bugün düğmenin sözde öğesi 0 (WL-9'da bir kez
  okundu, pin değil). Yazma.
- **CSP değişmez:** `style=`, `<style>`, `'unsafe-inline'` yok — tema bir dış stil dosyasıdır
  (`style-src 'self'`). `img-src 'self'` yalnız `<img>` çizen yanıtın politikasında
  (`TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`).

### Co-brand

Logo olan yüzeyde küçük bir satır kalır: **"taptime · punchless"** — bugünkü `punchless`
tonunda (10 px mono, büyük harf, geniş aralık, ink/70; porcelain'de 5,70:1). Tap ve sonuç
ekranında yeri **logonun altı** (kullanıcı kararı K-2b, 2026-10-02); panelde işletme adının
altı. Gerekçe: aktivasyonun GDPR cümlesi Taptime'ı işleyen olarak adlandırıyor; çalışanın her
gün gördüğü ekran aynı işleyeni göstermeli.

**Logo yokken:** accent varsa tap ekranının başlığındaki `taptime` **ink** olur (K-2a,
2026-10-02; porcelain'de 14,32:1) — ekranda tek vurgu rengi tenant'ın düğmesi kalır. Accent de
yoksa başlık bugünkü gibidir.

### Dokunulmaz — tenant markasından bağımsız (ADR 0023 §6)

Beş kaşe damgası ve durum→renk eşlemesi (kelime ink, renk çerçevede —
`TestCompiledCSS_StampWordIsInk` kelimenin rengini okur, accent'in damgaya girmemesini slot
testi okur) · tomato = hata/yıkıcı · saffron = FLAGGED/geç · `Notice` · docket + perforasyon ·
`.docket-label` · panelin birincil ve yıkıcı düğmeleri, sekme vurgusu, odak halkaları (tap
düğmesinin odak çizgisi tarayıcının rengidir, accent onu taşımaz) · sayfa zeminleri ve metin
tonları · yazı tipleri · sonuç ekranında accent yok (logolu tenant'ta yalnız Wordmark yerini
logo + co-brand satırına bırakır) · onay kutularının `accent-color`'ı (adı "accent" ama
tenant accent'i **değil** — `TestBrand_EveryNativeCheckboxAndRadioCarriesTheAccent`).

Panelde iki renk yan yana durur — yeşil panelin eylemlerinde, tenant rengi şeritte ve Account
önizlemesinin tap düğmesinde. Bu, yukarıdaki *"birden çok vurgu rengi"* yasağının **bilinçli**
istisnasıdır (K4): şerit eylem ya da bilgi taşımaz; önizlemedeki düğme bir eylem değil, tap
ekranının görüntüsüdür.

### Uygularken

- Yeni bir yüzeye logo ya da accent eklemek ADR 0023 §2'nin sayılı listesinin değişikliğidir;
  tap ve sonuç ekranında ayrıca CLAUDE.md §9'un sorusudur — önce sor. Üç §9 kararı (D-C,
  K-2a, K-2b) yalnız dört değişikliği kapsar: tap başlığında logo + co-brand, tap düğmesinde
  accent, sonuç başlığında logo + co-brand, logosuz ama accent'li tap ekranında ink `taptime`
  (ADR 0023 §7). Başka her tap/sonuç değişikliği yeniden sorulur.
- Şablon **yorumunda** yardımcı sınıf adı yazma, tarif et — `.templ` taranır, bu dosya
  taranmaz (yukarıdaki not).
- Accent'i sonuç ekranına, damgaya, docket'e ya da birincil düğmeye taşıma.
- Logoyu sayfada ayrı bir okumayla çizme: `img-src` kararı kabuğun okumasınındır.
- Logo için uzak URL kullanma — e-postada da (izleme pikseli).
- Logo pikselleri ve kırık logodaki `alt` durum kelimelerini taklit edebilir; bu kabul edilmiş
  bir risktir ([ADR 0005](../../../docs/adr/0005-kabul-edilen-riskler.md) risk 9) ve
  hafifletenleri bu bölümün sayılarıdır: 24 px yuva, en çok sütunun yarısı, tek *"Tap"*
  düğmesi, sonuçta logonun altındaki docket ve damga. Bu sayıları büyütmek riski büyütür.
