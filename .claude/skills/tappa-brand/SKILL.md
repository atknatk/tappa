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

⚠️ **Köşeler ve çipler — kullanıcı kararı, 2026-10-20 (panel yeniden tasarımı "The Pass", D2).**
Keskin köşe kuralı **adisyon (docket) ve kaşe damgası** içindir ve aynen kalır — imza motif budur;
fiş (receipt) ve mini docket da adisyondur. Bunların **dışındaki** yüzeyler tasarımdaki gibi
yuvarlatılabilir: kart, sayfa/çekmece (sheet), form alanı, düğme `rounded-xl` … `rounded-2xl` (kartlarda
`rounded-[18px]`), alan `rounded-[10px]`; **çip ve durum hapı `rounded-full`** olabilir. Yuvarlatma rengi
ya da durumu taşımaz; durum yine kelimede ve çerçevede. Teslim paketi:
`docs/design/panel-redesign/README.md` (yarıçap tablosu §4).

Anatomi:
- `bg-paper`, keskin köşe (`rounded-none`) veya en fazla `rounded-sm` — **yalnız docket ve damga**
  (yukarıdaki 2026-10-20 notu)
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
- **Düğme tahmini yönü söyler** (2026-10-22, M15-12 — kullanıcının §9 kararı 2026-10-21,
  ADR 0032 §B.1–§B.3, Q33): *"Clock in"* / *"Clock out"*, altında küçük bir *"Not right? Clock
  out"* (ya da *in*) — aynı formun ikinci `submit` düğmesi (`name="want"`), bağlantı ya da ikinci
  form **değil**; ink, altı çizili, en az 44 px yüksek, birincil düğmeden 24 px aşağıda (formun
  16 px aralığı + kendi 8 px üst boşluğu; Chrome'da 390/834/1200 px'te ölçüldü: aralık 24, yükseklik
  44, yatay kaydırma yok, porcelain'de 14,32:1), **accent almaz**. HTML'de statik gizli `want`
  yok; `tap.js` basılan değeri tek gizli alana yazar. Tahmin kapısından geçmeyen her durumda
  (anahtar kapalı, CMAC doğrulanmadı, plaket aktif değil, kişi aktif değil, iş günü çözülemedi,
  okuma başarısız) düğme eskisi gibi yönsüz *"Tap"*, altında hiçbir şey yok.
- Onay ekranında **buton yok**: *"All done — you can close this page."*
  Sonraki işlem zaten yeni fiziksel dokunuş gerektiriyor.
  **Tek istisna — talep kartı** (2026-10-26, M15-17; kullanıcının §9 kararı, ADR 0032 §C.1):
  kaydın ardından bir yarının eksik olduğu M15 sonrası bir çapa varsa (talep yok, K4'ün iki
  gösterimi içinde, `manual.MaxBackdate` içinde; yalnız `ok`/`flag`), docket'in **altında**,
  kapanış cümlesinin **üstünde** tek bir kart: kaydı adlandıran cümle (*"Yesterday you clocked in
  at 09:12 but there's no clock-out."* · aynı gün *"You clocked in at 11:00 and didn't clock
  out."* · daha eski *"On Mon 27 Jul you clocked in at 09:12 but there's no clock-out."* — gün,
  işletmenin bölgesindeki takvim günüdür; giriş eksikse aynı yapıda *"You clocked out at 12:00 but
  there's no clock-in."* / *"Yesterday you clocked out …"* / *"On Mon 27 Jul you clocked out …"* —
  kullanıcı kararı 2026-10-26), soru (*"What time did you leave?"* / *"What time did you come in?"*),
  saat alanı (vardiyanın gerçek ucu pencere içindeyse dolu,
  değilse **boş ve zorunlu**), *"Send to my manager"* ve *"You can skip this."* — **atlama düğmesi
  yok**. Kart `paper` zeminde yuvarlak kart (`line` kenarlık, 18 px), alan ortak `.field-input
  .field-input--mono` bileşeni (44 px; kayıt cümlesi ve sorun cümlesi `aria-describedby` ile bağlı,
  hatada `aria-invalid` ve `autofocus` — betik yok; yeniden gösterilen formda `h1` *"Check the time"*),
  düğme palet `tappa-green` zemin + `paper` yazı (7,73:1) ve 44 px — **accent almaz**, `.tap-button`
  değildir. Betik yok: sonuç ekranı konum okumaz. `flag`'te kapanış cümlesi kartın altında kalır.
  Kart, bu dokunuşun açık bıraktığı girişi soruyorsa o girişin ekran notu (*"your previous clock-in
  has no clock-out"*) gösterilmez; kayıt notu değişmez (yapısal: `tap.Decision.EmployeeNoteAsked`).
  Kartsız ekran bayt-aynı kalır. Gönderimin cevapları: *"Sent — your manager will review it."*
  (ikinci gönderim de), pencere dışı saat → kart yeniden (problem cümlesi saffron sol çizgiyle),
  *"This question has expired"*, *"Already resolved"*, pasif hesap, genel ekran — hiçbiri *"Nothing
  was recorded"* demez. Chrome'da 390 px'te ölçüldü: yatay kaydırma yok; kart metni paper'da
  16,17:1, *"You can skip this."* 6,05:1. Testler: `TestClaimScreens_AreByteIdenticalToTheGolden`,
  `TestResultScreen_TheCardDrawsTheLogoAndNoAccent`,
  `TestResultScreen_OnFlagTheClosingSentenceStaysBelowTheCard`.
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
> **D4 (2026-10-20, panel yeniden tasarımı — M12-12, M12-34):** bu bölüm *çalışanın* ekranları
> içindir. **Panelin** Venues & plaques sayfası artık plaketin **gerçek** QR'ını çizer
> (`components.PlaqueDrawing`; `<base>/t?tag=<uid>`, `sun.PlaqueQRURL`, sunucuda
> `internal/qrcode` ile satır içi SVG, `(*qrcode.Code).SVGPath`; sahte desen yok, `?src=qr` yok) ve
> yukarıdaki plaket metnini (*"Hold your phone here"*, *"Or scan — same thing"*, `PLAQUE 91AC`)
> taşır — yönetici duvara ne asılacağını görür. **Tap ve aktivasyon ailesi QR'dan hâlâ söz etmez**;
> `TestQRScreens_SayNothingAboutTheQRRoute` bu aileyi tarar (panel onun kapsamında değildir) ve
> bu karar aynen geçerlidir.
>
> **ADR 0026 (2026-10-04):** aşağıda anılan tur **kaldırıldı**; aktivasyon sihirbazı o
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

Kısa, sıcak, kendinden emin. *"Clocked in at 14:03."* — *"Your check-in operation
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

## Panel yeniden tasarımı ("The Pass") — bileşenler ve dersler

> Eklendi 2026-10-20 (M12-48). Normatif kaynak: `docs/design/panel-redesign/README.md` (§3 kısıtlar,
> §5 katalog, **§5.1 gerçekleşen adlar ve kayıtlı sapmalar**). Kararlar D1–D4 orada. Bu dosya
> Tailwind'in taramadığı bir dosyadır; aşağıdaki sınıf adları burada kural doğurmaz.

**Bileşen sınıfları — gerçekleşen adlarıyla** (`web/static/css/input.css`, `@layer components`):

| Aile | Sınıflar |
|---|---|
| Çip / segmentli | `.chip-link`, `.chip-link--on`, `.chip-count`, `.chip-group` |
| Form alanı | `.field-stack`, `.field-input`, `.field-input--mono`, `.field-help` |
| Kural merdiveni / anahtarı | `.ladder`, `.rung`, `.rung-no`, `.plate`, `.lock-glyph`, `.lock-mini` · `.rule-switch`, `--on`, `--off`, `.rule-switch-knob` |
| Hap | `.pill` + `--active/--invited/--deactivated` (veritabanı kelimeleri) · `.pill-tally` (bir `.tally--*` tonunun yanında) · `.mono-pill` (anahtar, webhook, teslimat; ton şablonda yanına yazılır) |
| Kartlar | `.panel-card`, `.stat-card` (`--spike`, `--open`), `.venue-card` (`--on`), `.queue-item` (`--current`), `.roster-row` |
| Kanıt | `.ev-tile` (`--ok/--bad/--none`), `.ev-badge` (`--ok/--none/--wide`) |
| The Roll | `.roll-*` — `.roll-table`, `.roll-row`, `.roll-cell` (`--worked/--other/--manual/--open/--none`), `.roll-mark`, `.roll-sum` … |
| Odd shapes | `.odd-*` — `.odd-card`, `.odd-title`, `.odd-chip` (`--open`), `.odd-callout` (`--alert/--warn`) |
| Manuel kayıt | `.manual-*` — `.manual-dircard`, `.manual-actbar` |
| The Spike | `.spike-*` — `.spike-step` (`--off`), `.spike-actions` |
| Kural değişikliği farkı | `.diff-del` / `.diff-ins` (M12-38 dalıyla gelir) |

**Bileşenler (templ):** `components.PanelNotice(tone, heading)` (ok "Done" / warn "Heads up" / alert
"Not done") · `components.PanelEmpty(where, heading, PanelEmptyCTA{Label, Href})` — başlığı **h2**,
kabuğun h1'inin altında · `pages.PanelProblem(PanelProblemView)` · `components.RailDocket` ·
`components.VerdictStamp` · `components.OneTimeSecret` (değer yanıtta **tam bir kez**) ·
`components.PlaqueDrawing` (gerçek QR — yukarıdaki D4 notu) · kova yardımcıları
`components.WidthPct` / `PctOf` / `SplitPct` / `TapeBar` (`components/buckets.templ`; sınıf adları
orada literal yazılı). Kabuk yuvaları `PanelChrome.Kicker` / `.Title` / `.Crumbs`; eylemler
`@pages.PanelActions(x) { @PanelShell(…) { … } }` sarmalayıcısıyla, alan değil. Sayfada **tek `<h1>`**.
`components.EmptyState` / `EmptyStateLabeled` M12-48'de kaldırıldı — geri getirme.

`components.Notice`, `components.Card`, `pages.Problem` ve `layout.*` **değişmez** (tap/aktivasyon
golden'ı); panel yalnız kendi `Panel*` bileşenlerini kullanır.

**Dersler — her biri en az bir kez kırmızı oldu:**

- **İşaretlemede iki ondalıklı keyfi değer yazma** (ör. 1,45'lik satır aralığını köşeli parantezle):
  koordinat taraması (`decimalDegreeShape`,
  `TestAnomaliesPages_CarryNoDecimalDegreeInEITHEROfTheOtherTwOShapes`) onu ondalık derece okur.
  Adlı bir adım (`leading-normal`, `leading-snug`) ya da bir bileşen sınıfı kullan.
- **templ, bir öğenin içindeki metnin baştaki boşluğunu düşürür:** iki parça arasında boşluk
  gerekiyorsa `{ " " + x }` (ya da ifadeden önce `{ " " }`) yaz.
- **Tailwind'in taraması sözcükseldir:** adı sıradan bir kelime olan bir katman sınıfı düzyazıdan
  derlenir (M12-08 ölçtü: `.chip`, `.field`, `.lock`, `.switch` hiçbir öğe taşımadan +2 479 B). Yeni
  sınıfa hiçbir cümlenin içermeyeceği bir ad ver.
- **`.tally*` seçicileri tonlara ayrılmıştır** (`TestTallyRules_NoToneRuleColoursTheWord`): bir ton
  çerçeveyi ve zemini boyar, kelimeyi asla; tally üzerindeki yerleşim kendi sınıfına gider
  (`.pill-tally`).
- **Dokunma hedefi** yeni bir basılabilir sınıfsa `panelTouchTargets`'a girer; ekranından önce
  tanımlanan sınıf `panelPendingTouchTargets`'ta bekler (bugün boş — tasarım gereği;
  `TestPanelScreens_PendingTouchTargetClassesReserve44pxInTheSource`).
- **Cümle içindeki bağlantı `.hit-link` taşır**, `inline-flex min-h-11` değil — o, satırı 44 px'e
  büyütüp cümleyi boşluklu satırlara böler (`TestPanelTemplates_ALinkInRunningTextKeepsTheLineBox`).
  ⚠️ `.hit-link::after` satır kutusunun ~11,5 px dışına taşar: başka bir kontrolün **12 px**
  yakınına koyma (ölçüldü: 4 px altındaki bir düğme üst ~7,5 px'ini bağlantıya kaptırdı).
- **Docket'in sınıf özniteliği tam `class="docket"`** kalır
  (`TestTransactionDocket_IsADocketAndCarriesEveryFieldTheCardAsksFor`; sonuç ekranıyla ortak) — tam
  docket'in dolgusu bu yüzden tasarımın tarifinden farklıdır (README §5.1).

## Public site — kabuk ve `.lp` bileşenleri

> Eklendi 2026-10-23 (LP-15, faz sonu). Normatif kaynak: `docs/design/landing-redesign/README.md`
> (§4 kısıtlar, §5 CSS stratejisi, **§6 pano adı → ürün adı**, §7 olgu pinleme, §8 404, §9 OG) ve
> `web/static/css/input.css`'in `.lp` bloğu (başlık yorumları LP-01 · LP-02 · LP-06 kararlarını taşır).
> Sayfalar: `/`, `/product`, `/security`, `/pricing`, `/for/restaurants`, `/for/production`,
> `/signup`, dört legal, `/docs/api`, `/docs/api/tr` + public 404.

**Kabuk — `pages.PublicChrome(PublicChromeOpts{…})`** (`web/templates/pages/publicchrome.templ`).
Tek DOM, her genişlik: header (`.lp-head` › `.lp-headIn` › `.lp-mark` + `.lp-headNav` içinde
`.navlink` ×3, `.lp-signin`, `.hbtn`), telefon menüsü (`details.menu`, `.mopen`/`.mclose`, `.mlink`;
betik yok, 768 px'ten itibaren gizli), telefonun alt çubuğu (`.mbar` › `.hbtn`), footer (`.lp-foot`,
`.fcol` Product + Legal), skip link (`.lp-skip` → `#main`). Bağlantılar **tek tablodan**:
`LandingNav`, `MountedPublicLinks(PublicProductLinks)`, `LegalPages` — yol elle yazılmaz;
`Current` eşleşen girdi `aria-current="page"` alır. Seçenekler:
- `OwnRoots` — gövdesi ürünün Tailwind yardımcılarıyla yazılmış sayfalar (signup, legal, API rehberi).
  Kabuk kendi `.lp` köklerini kurar (`.lp.lp-top` yapışan, `.lp.lp-topStill` yapışmayan,
  `.lp.lp-dock` alt çubuk), `<main>` her kökün **dışında** kalır. Gövde `PublicBodyFrame` (header'ın
  kutusu) ve `PublicBodyRing` (tek odak halkası) taşır.
- `InstantScroll` — yalnız API rehberi: `html:has(.lp.lp-instant)` yumuşak kaydırmayı kapatır
  (~50 000 px sayfada içindekiler bağlantısı saniyelerce kayıyordu).
- `StaticHeader` + `NoPhoneBar` + `SignInPrompt` — yalnız signup.

**`.lp` kapsamı ve sıfırlama kuralı.** Her public kural `.lp` altında, `@layer` dışında düz CSS'tir
(ağaç budaması yok; `TestLandingCSS_EverySelectorIsScopedUnderLp`). `.lp * { margin:0; padding:0 }`
Tailwind boşluk yardımcılarıyla aynı özgüllükte ve dosyada sonra gelir → **`.lp` kökü içinde her
`p-*`/`m-*` yardımcısı kaybeder** (ölçüldü: cookie sayfası 390'da 685 px, rehber 5 169 px kaybetti).
Kural: `.lp` sınıflarıyla çizilen gövde `.lp` kökünde; yardımcılarla çizilen gövde asla `.lp`
içinde değil (`OwnRoots`; `TestPublicChrome_UtilityBodiesStayOutsideTheLPRoot`). Ürünün sahip
olduğu adlar `lp-` önekli: `.lp-button` (`.btn` değil), `.lp-slip` (`.docket` değil), `.lp-lbl`,
`.lp-eyebrow`, `.lp-ico`. Her renk `.lp` üstünde token (`rgb(var(--ink-ch) / a)`), ham hex ve
`style=` yok (`marketingCSP`: `style-src 'self'`).

**Tek odak halkası:** `.lp :focus-visible { outline: 2px solid var(--ring); outline-offset: 2px }`.
`--ring` = ink (porselen 14,32:1, paper 16,17:1); koyu zeminde (`.band--ink`, yeşil `.ondark`)
paper (yeşilde 7,73:1, ink'te 16,17:1). Eski 3 px saffron halka 2,32:1 ölçtüğü için gitti.
Yardımcı gövdelerde aynı halka `PublicBodyRing` ile (`TestPublicPages_UtilityBodiesDrawTheOneRing`).
Her çizilmiş durumun (`:hover`, `:active`, `[aria-current]`) kuralı var; disabled hücresi
ulaşılamaz (signup'ın gönder düğmesi ürünün `.btn`'i ve sayfa betiksiz), bu yüzden `.lp` bloğu
disabled kuralı yazmaz (`TestLandingCSS_EveryDrawnStateHasItsRule`).

**Dokunma tabanı:** basılan her sınıf `min-height: 44px`; menü bağlantıları ve tam boy düğmeler
48 px. Pano 40 px çizdiği yerde (footer bağlantısı) ürünün tabanı kazanır
(`TestLandingCSS_EveryPressableClassReservesTheTouchFloor`).

**Animasyon yok** (README O2): `@keyframes`/`animation` public blokta yasak
(`TestLandingCSS_ThePublicSiteDeclaresNoAnimation`). Kalan tek hareket durum geçişidir
(`details.qa .pl` dönüşü, `.fitcard` kenarı) ve `prefers-reduced-motion: no-preference` içindedir.
**İstisna:** hero'nun fişi `landing.js` ile kendiliğinden kelime değiştirir — bu yüzden
`.heroPause` düğmesi var (WCAG 2.2.2; panoda yok, orkestratör kararı). Demo diyaloğu tarayıcının
`showModal()`'ı ile açılır, hareketsiz.

**Kaşe dili:** public site kendi kaşesini çizmez — ürünün `.stamp .stamp--approved/--flagged/
--rejected/--ignored`'ı (README O7; kelime her zaman ink, `TestCompiledCSS_StampWordIsInk`).
Public'e özgü olan yalnız iki eğim (`.lp .stamp--rejected` −10°, `--ignored` −4°) ve küçük boy
`.lp-stamp-sm`. Fiş: `.lp-slip` (alt kenarı perforeli), tap ekranı çizimindeki bilet `.tapdocket`.

**Örnek veri kuralları** (`landingview.go` "the panel", `forrestaurantsview.go`,
`forproductionview.go`):
1. Örnek kişi **uydurmadır ve seed'de olmayan** biridir (seed kişisinin durumu/rolü çizimle
   çelişirdi); mekânlar ve KM departmanları seed'inkidir (K1, "from our demo data").
2. Örnek **görünür biçimde "sample" der**: `LandingPanelSampleNote`, `ForProductionSampleNote`,
   erişilebilir ad "Sample screen: …", "The Roll · sample", hero fişinde "Sample record" ve sahnenin
   adı "Sample scene: …"; not çizimin **durduğu yerde** olur (/product'ta her çizimin altında).
   Hero kaydı sitenin başka bir çiziminin aynı kişisidir ve o çizimin kanıtını docket'in
   kelimeleriyle gösterir ("IP yes · GPS no").
3. Örnek **motordan geçer**: her örnek kayıt `tap.Decide`'dan geçirilir, damga/puan/not ürünün
   üreteceğiyle aynı olmalı (`TestLandingPanel_TheSampleIsWhatTheProductWouldDraw`). Çizim kontrol
   değildir (`role="img"`, düğmeler `span`).

**"Her cümle pinli" düzeneği:** sayfa metni şablonda yazılmaz, view dosyasında veridir —
`Line{Text, Facts}`, `Question`, sayfaya özgü yapılar. Üç sözlük: `Fact` (ürün olgusu),
`Source` (motor olgusu), `Anchor` (şema/alan olgusu). Her bildirilen olgu üründen **türetilir**
(kod, şema, migration, CSS, config ya da kararın yazılı olduğu belge — asla sayfanın kendi cümlesi)
**ve** en az bir render edilen cümle tarafından talep edilir. Yeni sayfa: `pages.PublicPages`
satırı + `Mount`'ta rota, `publicPagesCopy` girdisi, türetme tablosu `pageFactDerivations`'a
eklenir (`internal/handler/marketing_facts_test.go`); değer **kendi URL'sinde** render edilmeli
(`TestPublicPages_EveryValueIsRenderedOnItsOwnPageAndRestsOnAFact`,
`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed`). Pin cümleyi doğru yapmaz — kayma
cırcırıdır; her cümle yine üründen okunur.

**Yasaklı ifadeler** (her biri bir testte; bir public sayfada geri gelirse kırmızı):
- Üstünlük: `superiorityClaim` — "safer/better/faster/cheaper … than", "the best", "industry-leading",
  "unbeatable". Mekanizma karşılaştırılır, kazanan ilan edilmez.
- Ölçülmemiş sayı: "under 10 minutes", "30 seconds", "under two seconds", "one click", "about €" …;
  ürün olmayan yetenek: "live headcount", "live dashboard", "import your staff", "csv import",
  "unlimited locations", "daily reports" …; hiçbir sayfada (baytlar dahil) "guarantee".
- Sayfa kartlarının geri çektiği cümleler: `securityRefusedPhrasings` ("Four proofs on every single
  tap", "never silently approved", "no NFC" …), `productRefusedPhrasings`,
  `restaurantsRefusedPhrasings`, `forProductionRefusedPhrasings` ("per plaque"/"per tag" — debounce
  kişi bazlıdır) — her birinin gerekçesi haritada yazılı.
- İç kod adı yok — sayfa yalnız kullanıcıya görünen markayı ("Taptime") taşır
  (`TestLanding_ShowsNoUserFacingInternalName`); `/v1` mount'luyken "no API" cümlesi yok
  (`TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted`).

**Public 404** (ADR 0034, `internal/handler/notfound.go`): kabuk + tek cümle ("There is no page at
this address.") + "Back to the home page", status 404, `noindex`, `no-store`, her yol ve
ziyaretçi için aynı bayt (yol yazılmaz). Yalnız `GET`/`HEAD` ve `plainNotFoundRoots` dışındaki
yollarda; `/admin`, `/t`, `/activate`, `/api`, `/brand`, `/static`, `/operator`, `/v1`, probe'lar ve
`/dev` altında net/http'nin düz 404'ü kalır (marka ret özdeşliği, operatör yüzeyinin görünmezliği).

**OG kartları** (K3, 2026-10-23 güncel; README §9): `web/static/og/` altında dokuz PNG,
1200×630 (`landing`, `product`, `pricing`, `security`, `restaurants`, `production`, `signup`,
`legal`, `api`). Pano elemanı + `shareCardBoardEdits` düzeltmeleri, yerelde headless Chrome ile
self-hosted fontlarla çizilir ve commit edilir; test her PNG'nin sha256'sını düzeltilmiş kaynağın
sha256'sıyla birlikte pinler (`TestShareImages_AreDrawnFromThePinnedSources`) — ikisi birlikte
yenilenir. Görselde fiyat yok, kişiler uydurma. `og:image` mutlak URL'si `TAPPA_BASE_URL`'den;
üçüncü taraf muafiyeti yalnız o origin'in `/static/og/` yolları
(`TestShareImageExemption_IsNarrow`). Head etiketleri yalnız `marketingDocument` üzerinden — tap/
aktivasyon head'leri golden'la bayt-aynı.

## Yapma

Emoji ikon seti (marka mesajları hariç) · yuvarlatılmış adisyon ya da kaşe damgası ·
gradient · glassmorphism · sallanan animasyon · stok illüstrasyon · birden çok vurgu rengi ·
mono olmayan sayı · **işlem kaydı** için adisyon yerine düz tablo satırı.

*(2026-10-20, D2: bu listede "yuvarlak hap butonlar" yazıyordu; kullanıcı kuralı güncelledi — çipler ve
durum hapları `rounded-full` olabilir, düğmeler ve kartlar yuvarlatılabilir; yasak artık yalnız
docket ve damganın köşesidir. Kişi listesi (People) ve aggregate kartları kayıt değildir; işlem kayıtları
yine adisyondur.)*

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
| Sonuç ekranı | tap ekranınınkiyle aynı başlık | **yok** — tema `<link>`'i 0, renkler durumu anlatır; talep kartının düğmesi de paletin (M15-17) | işletme adı | WL-9 — `TestResultScreen_DrawsTheLogoAndNoAccent`, M15-17 — `TestResultScreen_TheCardDrawsTheLogoAndNoAccent` |
| Panel kabuğu | başlıkta 32 px kutu + işletme adı + co-brand | **4 px şerit** | **boş** (`alt=""`) — ad logonun yanında görünür metin | WL-8 — `TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection` |
| Account → *"Your brand"* önizlemesi | önizlemedeki tap başlığı, kaynak `/admin/brand/logo/{sha}` | önizlemedeki tap düğmesi — **yalnız kaydedilen** accent | işletme adı | WL-7 — `TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit` |
| Aktivasyon ailesi — sihirbazın dört adımı, *"Activation complete"*, *"already set up"* | tap ekranının başlığı (24 px yuva + co-brand) — **yalnız VIES-doğrulanmış** işletmede; sihirbazda `/activate/logo/{sha}`, oturumlu iki ekranda `/t/logo/{sha}` | **adım 1–3'ün birincil düğmesinin zemini**; accent varken ilerleme çubuğu, 01/02/03 ve işaretli onay kutusu **ink** | işletme adı | WL-13 — `TestActivationScreens_ABrandedBusinessGetsTheTapScreensHeaderAndTheme` |
| Aktivasyonun problem ekranları ve *"phone already in use"* onayı, tap problem ekranları, giriş, AdminChoose, parola sıfırlama, landing, legal, signup, operatör | yok | yok | — | Taptime (aktivasyon problem/onay: WL-13 — `TestActivationFailureScreens_StayTaptimesForABrandedInvitation`) |
| E-posta | faz 1'de yok | yok | — | K8: markadan en çok işletme adı, gövdede; adın gösterilip gösterilmeyeceği EM-7'nin bekleyen kullanıcı kararı (WL-11 SES akışıyla bekliyor) |

- **Başka işletmenin plaketi:** tap ve sonuç ekranı Taptime varsayılanıyla açılır — `<img>`,
  tema bağlantısı ve `/t/logo/` 0 (`TestTapPage_AnotherBusinesssPlaqueShowsNoBrand`,
  `TestResultScreen_NoBrandWhereThePlaqueIsNotThisBusinesss`).
- **Marka yoksa bayt-aynı:** tap, sonuç ve aktivasyon ailesinin 38 render'ı (WL-13'te 33 → 36,
  M15-12'de 36 → 38: tap ekranının iki yönlü şekli; aynı kart yönlü on sonuç golden'ını başlık
  kelimesiyle yeniden yazdı)
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
- **Tap düğmesinin sınıfı yedi `class` özniteliğinde** (2026-10-09'da sayıldı: `tap.templ`'de
  iki — gönderen yüz ve önizleme yüzü —, aktivasyon ailesinde beş: sihirbazın *Next*, *Agree and
  continue*, *I'm ready*'si, problem ekranının *Try again*'i, onay ekranının *Continue anyway*'i;
  2026-10-06'da sekizdi, tur ADR 0026 ile kalktı; 2026-10-22'de M15-12 ile yine sekiz: `tap.templ`'de
  üç — yönsüz ve yönlü gönderen yüz, önizleme yüzü), ama accent'i sınıf değil **sayfanın tema
  bağlantısı** taşır: tema tap ekranına, panel kabuğuna ve (WL-13) aktivasyon ailesinin altı
  markalı ekranına, `app.css`'ten hemen sonra bağlanır; problem ve onay ekranlarındaki iki kullanım
  yeşil kalır (ADR 0023 §2, §4). Panelin içindeki Account önizlemesinin düğmesi accent'i bu yüzden
  **bilerek** alır.

### Aktivasyon ailesi (ADR 0023 WL-13 notu — kullanıcı kararları, 2026-10-09)

- **Şekil tap ekranınınki:** `layout.BrandHeader` (logo 24 px yuvada + co-brand; logo yokken
  accent varsa ink `taptime`) ve tema bağlantısı. Kabuk `layout.ActivationPage(title, Brand)`
  (betiksiz), adım 4 `PageWithScript(…, Brand)`; `layout.Page` marka almaz — problem ve onay
  ekranları onu çağırır.
- **Logo yalnız VIES-doğrulanmış işletmede** (`tenants.vat_verified IS TRUE`; FALSE ve NULL
  "doğrulanmamış"). Kapı okumadadır (`BrandReader.ActivationBrand`, `GetTenantActivationLogo`'nun
  `WHERE`'i), sayfada değil: doğrulanmamış işletmede ekran `<img>` yazmaz, `img-src` adlandırmaz.
- **Hangi rota:** sihirbaz (oturum yok) `/activate/logo/{sha}` — işletme davet çerezinden;
  *"Activation complete"* ve *"already set up"* (oturumlu) `/t/logo/{sha}`.
- **Kimin markası:** sihirbaz davetin işletmesinin (telefonda başka işletmenin oturumu olsa da);
  oturumlu iki ekran oturumun işletmesinin.
- **Accent'in slotu sihirbazın 1–3. adımlarının `.tap-button`'ı**; adım 4'te birincil düğme yok.
  Accent varken ilerleme çubuğunun tamamlanmış segmentleri, 01/02/03 ve işaretli onay kutusu
  (kenar, %5 zemin, kutunun kendi rengi) **ink** — ekranda tek vurgu rengi tenant'ın düğmesi
  (K-2a'nın uzantısı). Ink sınıfları şablonda seçilir; tema gövdesi değişmez.
- **Değişmez:** bekleme ekranının plaket çizimi (halka + yeşil *"taptime"*) — duvardaki plaket
  Taptime'ındır (ADR 0023 §9 madde 4); damga ve `Notice` renkleri; aktivasyonun problem ve onay
  ekranları (marka yalnız kullanılabilir bir davetten bilinir — ölü linki canlıdan ayırırdı).
- **Okunamazsa** Taptime'ın sayfası + tek ERROR; bugün reddedilen accent → accent ve ink yok,
  logo kalır, WARN (tap ekranının kuralı).

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
- Ekran hâlâ **tek birincil düğme**, en az 64 px; markanın tap ekranına getirdiği tek yeni metin
  logonun `alt`'ıdır (ADR 0023 §6 madde 10). *(2026-10-22, M15-12: burada "kelimesi 'Tap'"
  yazıyordu; düğme artık tahmini yönü söyler — *"Clock in"* / *"Clock out"*, kapı kapalıyken
  *"Tap"* — ve yönlü ekranda altında accent'siz *"Not right? …"* durur; ADR 0023 §6 madde 10'un
  tarihli notu, ADR 0032 §B.1.)*
- **Kırık logo:** logo isteği 404 alırsa (ör. logo değişti, eski sayfa açık) tarayıcı 24 px
  yuvada kırık görsel simgesini ve `alt`'ı — işletme adını — kırpılmış gösterir. `alt` = ad
  kabul şartının bedeli, kod değişmez (WL-9 sınır 11); ad serbest metin olduğu için bu,
  [ADR 0005](../../../docs/adr/0005-kabul-edilen-riskler.md) risk 9'un (marka taklidi) bir
  biçimidir.
- Logo porcelain zeminde durur: çerçeve, gölge, yuvarlatma yok. `<img>` ile gelir — CSS arka
  plan görseli değil (`alt` taşımaz, aynı CSP iznini ister).

### Panel kabuğu (ADR 0023 WL-8 notu)

- ⚠️ **2026-10-20 — "The Pass" kabuğu (kullanıcı onaylı tasarım).** Başlık artık panelin
  **yan kolonunun** tepesindedir: < 768 px telefonun porselen üst çubuğu, 768–1199 px 88 px'lik
  ink ikon rayı, ≥ 1200 px (`desk` ekranı, `tailwind.config.js`) 248 px'lik ink kenar çubuğu.
  Başlık belgede **bir kez** durur; slotlar aynı: 4 px şerit (yalnız tema bağlıysa, başlığın ilk
  öğesi, kolonun tepesi boyunca) ve 32 px logo kutusu — logo ink zeminde **paper bir karonun**
  içindedir (logolar açık zemin için çizilir), rayda yalnız küçülebilir. Ad ve co-brand
  `side-name` / `side-coline` sınıflarında: porselende ink (14,32:1) ve ink/70 (5,70:1), ink
  üzerinde paper (16,2:1) ve paper/70 (8,5:1); rayda ad görsel olarak gizli ama okunur.
  Markasız kilit `pages.PanelWordmark` (ink üzerinde paper "taptime"/"punchless", rayda "t.").
  Logosuz markalı işletmede rayın işareti adın ilk harfidir ("t." yalnız markasızda).
  Aşağıdaki maddeler (karar 3, 6, 9) aynen geçerlidir; "porcelain'de" diyen kontrast sayıları
  yalnız telefon üst çubuğu içindir, ink üzerindeki sayılar bu maddededir.
- *Markalı* = kapıdan **bugün** geçen accent **ya da** logo (karar 3). Markalıda
  `PanelWordmark`'ın yerine: 4 px şerit (yalnız accent geçiyorsa; başlığın ilk öğesi,
  `aria-hidden`) · logo (varsa) · işletme adı (`side-name`) · altında co-brand
  (`side-coline`). **Yalnız accent'li** işletmede de ad ve co-brand çizilir. Ad ve co-brand
  paragrafları tam sınıf listeleriyle pinli; renkleri ve oranları yukarıdaki maddede.
- **Logo kutusu: 32 px yükseklik, en çok 192 px genişlik**, oran korunur, saklanandan büyük
  çizilmez — küçük logo büyütülüp bulanıklaşmaz; 512×128 → 128×32
  (`TestPanelLogoOf_KeepsTheProportionsInsideTheSlot`, 1–512 × 1–512 taraması; karar 6).
- **`alt=""`** (karar 9): ada eşit bir `alt` ekran okuyucuda adı iki kez okutur ve kırık logoda
  adı kutunun içine basıp sayfayı iter — 114 karakterlik adla kutu 128×216, içerik +145 px
  (ölçüldü). Boş `alt` ile kırık logo kutunun dışına **0 px** basar (sınır 5).
- Şerit yan kolonun tepesi boyunca 4 px: rayda 88 px, kenar çubuğunda 248 px, telefonda üst
  çubuğun tam genişliği; tenant rengi yalnız orada. Birincil düğme yeşil, geçerli bölüm işareti
  **paper bir hap** kalır — accent geçerli işareti hiçbir genişlikte boyamaz (K4; ADR 0023 WL-8
  notu eki, 2026-10-20).

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
tenant accent'i **değil** — `TestBrand_EveryNativeCheckboxAndRadioCarriesTheAccent`). *WL-13
istisnası (2026-10-09, kullanıcı kararı):* aktivasyon sihirbazında accent varken işaretli onay
kutusu ink (`accent-ink`; çerçevesi ve zemini de ink) — tenant accent'i kutuya girmez, yanında
tenant accent'i durduğu için Taptime yeşili ink'e döner (ADR 0023 §6 madde 9).

Panelde iki renk yan yana durur — yeşil panelin eylemlerinde, tenant rengi şeritte ve Account
önizlemesinin tap düğmesinde. Bu, yukarıdaki *"birden çok vurgu rengi"* yasağının **bilinçli**
istisnasıdır (K4): şerit eylem ya da bilgi taşımaz; önizlemedeki düğme bir eylem değil, tap
ekranının görüntüsüdür.

### Uygularken

- Yeni bir yüzeye logo ya da accent eklemek ADR 0023 §2'nin sayılı listesinin değişikliğidir;
  tap ve sonuç ekranında ayrıca CLAUDE.md §9'un sorusudur — önce sor. Üç §9 kararı (D-C,
  K-2a, K-2b) yalnız dört değişikliği kapsar: tap başlığında logo + co-brand, tap düğmesinde
  accent, sonuç başlığında logo + co-brand, logosuz ama accent'li tap ekranında ink `taptime`
  (ADR 0023 §7). Aktivasyon ailesinin üç kararı (2026-10-09) üç değişiklik daha kapsar (ADR 0023
  §7'nin WL-13 eki: v–vii). Sonuç ekranının talep kartı ayrı bir §9 kararıdır (ADR 0032 §C.1,
  M15-17; yukarıda). Başka her tap/sonuç/aktivasyon değişikliği yeniden sorulur.
- Şablon **yorumunda** yardımcı sınıf adı yazma, tarif et — `.templ` taranır, bu dosya
  taranmaz (yukarıdaki not).
- Accent'i sonuç ekranına, damgaya, docket'e ya da panelin birincil düğmesine taşıma
  (aktivasyon sihirbazının birincil düğmeleri `.tap-button`'dır ve accent'i WL-13'ten beri alır).
- Logoyu sayfada ayrı bir okumayla çizme: `img-src` kararı kabuğun okumasınındır.
- Logo için uzak URL kullanma — e-postada da (izleme pikseli).
- Logo pikselleri ve kırık logodaki `alt` durum kelimelerini taklit edebilir; bu kabul edilmiş
  bir risktir ([ADR 0005](../../../docs/adr/0005-kabul-edilen-riskler.md) risk 9) ve
  hafifletenleri bu bölümün sayılarıdır: 24 px yuva, en çok sütunun yarısı, tek birincil
  düğme, sonuçta logonun altındaki docket ve damga. Bu sayıları büyütmek riski büyütür.
