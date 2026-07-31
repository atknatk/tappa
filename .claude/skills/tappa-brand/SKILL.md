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
- **Kaşe damgası**: sağ üstte hafif eğik (`-rotate-6` … `-rotate-12`), çift
  çerçeveli, yarı saydam (`opacity-80`), harf aralığı geniş. Mürekkep izlenimi —
  düz badge değil.

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
- Etkileşim HTMX ile: `hx-post`, `hx-target`, `hx-swap`. İstemci state'i
  gerektiren bir şey istiyorsan önce dur ve gerçekten gerekli mi sor.
- Erişilebilirlik: durum **asla** yalnız renkle anlatılmaz — damga metni de var.
  Kontrast AA. Dokunma hedefi ≥ 44px. `prefers-reduced-motion` saygılı.
- Karanlık tema **yok** (şimdilik) — plaket ortamı aydınlık, yarım iş yapma.

## Yapma

Emoji ikon seti (marka mesajları hariç) · yuvarlak hap butonlar · gradient ·
glassmorphism · sallanan animasyon · stok illüstrasyon · birden çok vurgu rengi ·
mono olmayan sayı · adisyon yerine düz tablo satırı.
