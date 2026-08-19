---
name: tappa-sun
description: NTAG 424 DNA SUN/SDM doğrulaması — URL biçimi, AES-128 CMAC hesabı, monoton sayaç (ctr) ve atomik replay koruması, PICC data şifre çözümü, anahtar saklama, test vektörleri. internal/sun, tap uç noktası, tag kaydı/değişimi, "etiket", "NFC", "CMAC", "ctr", "replay", "SUN", "SDM" geçen her işte oku.
---

# SUN doğrulaması (NTAG 424 DNA — SDM)

SUN = *Secure Unique NFC*. Tappa'nın **proof of moment** kanıtı: bu URL'nin
şu anda, gerçek çipe fiziksel dokunuşla üretildiğini gösterir. Diğer üç kanıt
(oturum, IP, GPS) taklit edilebilir bağlamdır; SUN edilemez.

## Nasıl çalışır

Çip her okutmada NDEF URL'sini kendi içinde yeniden yazar: okuma sayacını
1 artırır ve chip'e gömülü AES-128 anahtarla bir CMAC üretir.

```
https://time.tappa.mt/t?tag=91AC7E5500000A&ctr=000641&cmac=8F2A4C19D3B05E77
```

| Parametre | Format | Anlam |
|---|---|---|
| `tag` / UID | 7 byte hex (14 hane) | çip kimliği → hangi plaket, hangi lokasyon |
| `ctr` | 3 byte hex (6 hane), big-endian | okuma sayacı, **her okutmada +1**, geri gitmez |
| `cmac` | 8 byte hex (16 hane) | AES-CMAC'in kısaltılmış hali |

`ctr` 24 bit → 16.777.215'te sarar. Günde 20 tap ile ~2000 yıl; yine de sarma
davranışını belgele ve tag ömrü boyunca aşılmayacağını doğrula.

## Doğrulama algoritması

```
1. UID'yi çöz → tags tablosundan kaydı bul.
   Yoksa            → reject (bilinmeyen etiket)
   status != active → reject (lost / retired)
2. Etiketin AES anahtarını KEK ile aç (aes_key_ref → plaintext key, bellekte).
3. SDM session MAC anahtarını türet: CMAC(K_sdmfileread, SV2)
   SV2 = 3C C3 00 01 00 80 || UID(7) || SDMReadCtr(3, LSB-FIRST)
   ⚠️ SDMReadCtr, URL'deki `ctr`'ın BAYT-TERSİDİR. URL MSB-first, SV2 LSB-first.
      (AN12196 rev. 1.8 §4.3 Tablo 2 adım 4/7, s. 10 + §4.4.1 s. 11 aynı UID)
4. Mesajı hesapla: full = AES-CMAC(K_session, sdmMacInput)
   SDMMACInputOffset yoksa (yalnız UID+ctr mirroring) mesaj boştur.
5. Kısalt: mac = full[1], full[3], full[5], … full[15]   (tek indeksli 8 byte)
6. subtle.ConstantTimeCompare(mac, gelen_cmac) → eşit değilse reject.
7. ATOMİK sayaç ilerletme:
      UPDATE tags SET last_ctr = $2 WHERE uid = $1 AND last_ctr < $2 RETURNING uid
   0 satır → replay ya da eşzamanlı yarış → reject.
8. Buraya kadar geldiyse sun_valid = true.
```

**Adım 5'i atlama.** NTAG 424 tam 16 byte CMAC'i değil, tek indeksli byte'lardan
oluşan 8 byte'lık kısaltılmış halini yayınlar. Tam CMAC ile karşılaştıran
uygulama her zaman başarısız olur — ve bu, hata ayıklarken en çok zaman
kaybettiren noktadır.

## Kritik kurallar

**Sıra sabittir: CMAC doğrula → SONRA sayacı ilerlet.** Ters sırada saldırgan
geçersiz CMAC'li isteklerle sayacı ileri sürüp meşru tap'leri reddettirebilir (DoS).

**Sayaç ilerletme tek atomik ifadedir.** Şunların hiçbiri kabul edilmez:
- `SELECT last_ctr` → Go'da karşılaştır → `UPDATE` (klasik TOCTOU)
- `sync.Mutex` ile korumak (tek process varsayımı; ikinci instance açılınca çöker)
- `>=` kullanmak — aynı `ctr` iki kez geçer, replay'in ta kendisi

Doğrusu: `WHERE last_ctr < $2` ile **strict** karşılaştırma, `RETURNING` sonucu
kontrol edilir.

**Sabit zamanlı karşılaştırma.** `bytes.Equal` veya `==` değil,
`crypto/subtle.ConstantTimeCompare`. CMAC karşılaştırması timing sızdırırsa
saldırgan byte byte imza üretebilir.

**İleri atlama toleransı.** Etiket bizim bilmediğimiz okumalarla ileri gitmiş
olabilir (biri telefonunu değdirdi ama sayfayı açmadı). `ctr > last_ctr` yeterli;
tam olarak `last_ctr + 1` **beklenmez**. Ancak sayaç boşluğu şüphelidir (eşik politikadan gelir: `base:ctr-gap-review`)
→ kaydı `flag` ile al ve güvenlik uyarısı üret.

**Anahtar saklama.** Plaintext AES anahtarı ne repoda, ne log'da, ne hata
mesajında, ne de DB'de düz olarak bulunur. `tags.aes_key_ref` KEK ile sarmalanmış
(`TAPPA_TAG_KEK`, envelope encryption) değeri gösterir. Anahtar bellekte
yalnızca doğrulama süresince yaşar.

## QR kanalı — SUN yok

> ⚠️ Başlık 2026-08-01'de *"QR fallback"*tan değiştirildi. **QR bazı çalışanlar
> için yedek değil, KALICI yoldur:** iPhone X ve öncesi arka planda NFC etiketi
> okuyamaz, o telefonda NFC URL'i **hiç açılmaz**. Belge ve arayüz dili bunu
> yansıtmalı (bkz. skill `tappa-brand` → *"Plaket baskısı — NFC + QR"*).

Plakette NFC ile birlikte QR da basılır. QR statiktir → `ctr` ve `cmac` yoktur →
`sun_valid = false`, `channel = 'qr'` (sunucu türetir; istemci beyanı **hiçbir
yüzeyden** geçmez — ölçüldü, M5-08).
Bu kanalda sunucu **IP eşleşmesini zorunlu tutar**; GPS tek başına yetmez →
karar `flag`. (Karar Q15; baseline politikası `base:qr-requires-ip`, tenant
değiştirebilir. Gerekçe: QR fotoğraflanır, süresiz geçerlidir ve hiçbir fiziksel
dokunuş kanıtı taşımaz — sahte konumla evden APPROVED alınabilirdi.)

🔴 **GERİ ÇEKİLEN CÜMLE (2026-08-01, M5-08 denetimi).** Burada
*"QR asla NFC ile aynı **güven seviyesine** çıkarılmaz."* yazıyordu. **Lafzı
yanlıştı** ve ölçüldü: **trust SAYISI kanalı hiç ayırmıyor** — aynı kanıtla
(IP+GPS) QR **100**, NFC **100**; yalnız GPS'te QR **50**, NFC **50**. Sebep
tasarım: `trustScore` (`internal/domain/tap/decide.go`) kanal terimi **taşımıyor**,
çünkü CLAUDE.md §5 formülü normatif yazıyor (`20 + 50 IP + 30 GPS`) ve trust
**kanıtı** ölçer, **sonucu** değil.

**Doğru ifade — KANIT TAVANI:** QR aynı kanıtla **asla NFC'nin sonucuna
çıkamaz**. Yalnız GPS ile bir QR tap'i **hiçbir zaman insansız `ok` olamaz**
(`base:qr-requires-ip` → `flag`); aynı fix'le bir NFC tap'i `ok` olur
(`base:gps-only-allow`). Üçü de uçtan uca pinli:
`internal/handler/qr_db_test.go → TestQRDB_WithoutAnIPMatchGPSAloneIsNotEnough`.

**Kullanıcı kararı (2026-08-01):** §5'in formülü **değişmiyor**; ayrımı
`transactions.channel` taşır (`nfc | qr | manual`), ve **trust'ın gösterildiği
her yüzeyde kanal da gösterilecek** (M6-01 / M6-04 devri) — yoksa müdür
`trust 100` görüp NFC sanar. Gerekçe ve ölçümlerin tamamı:
[docs/plan/m5-tap-akisi.md → M5-08 kart düzeltmesi md. 3](../../../docs/plan/m5-tap-akisi.md).

⚠️ **NFC'nin verdiği ama QR'ın yapısal olarak veremediği şey:** *"bir sonraki
işlem yeni fiziksel dokunuş gerektirir"*. NFC'de bunu **atomik sayaç ilerletmesi**
sağlıyor — tek dokunuşun URL'i ikinci kez POST edilemez. **QR'da ilerletilecek
sayaç yok**, dolayısıyla tek fren **60 sn'lik person-debounce**'tur.

🔴 **VE O FREN 2026-08-01'E KADAR ÇALIŞMIYORDU** (M5-08'de ölçüldü). Debounce
girdisi yalnız önceki satırın `occurred_at`'ine bakıyordu, o alan da **istemciden
gelebiliyor** → geçmiş bir zaman beyan eden tekrar devasa bir gap bildiriyor ve
guardrail **hiç ateşlenmiyordu**. Ölçüm: tek taranmış QR bağlamı, **0,51 sn'de 20
yön taşıyan satır, 0 `ignored`**; aynı numara NFC'de **1 satır + 19
replay-reject** (freni sayaç tutuyor).

🔴 **VE KUSUR ÜÇ KATMANLIYDI** — ilk düzeltme yalnız birincisini kapattı,
denetim diğer ikisini ölçtü:

| Katman | Ne yapılıyor | Ölçüm (20 POST) |
|---|---|---|
| **MESAFE** | geçmiş bir `occurred_at` beyan et | 20 sayılan satır |
| **SEÇİM** | kişinin **mevcut en yeni satırının altına** beyan et → öncül hiç ilerlemez (`GetLastTransactionForEmployee` `ORDER BY occurred_at`) | 20 sayılan satır |
| **İŞARET** | **tek** bir ileri tarihli POST: `sys:occurred-at-bound` reddeder ama §4.6 satırı **yazar**, o satır sıralamayı kazanır, gap **negatif** olur, guardrail `gap >= 0` istediği için hiç ateşlemez | sonraki 20 dürüst tap → **20 `ok`** (flag bile değil) |

✅ **Kural (kullanıcı kararı 2026-08-01,
[ADR 0006](../../../docs/adr/0006-debounce-iki-kosullu-zaman.md)):**

```
gap = min( now − LastForPerson.OccurredAt ,  SecondsSinceLastRecordedTap )
```

**Sunucu bacağı ayrı bir sorgudan gelir** — `SecondsSinceLastRecordedTap`:
`ORDER BY created_at DESC`, `channel IN ('nfc','qr')`, ve **yaşı SQL hesaplar**
(`EXTRACT(EPOCH FROM (clock_timestamp() − created_at))`). Go hiçbir DB
timestamp'ini kendi saatinden **çıkarmaz**: öyle yapan ilk sürüm 4. katmanda
çöktü — uygulama `now`'ı kilidi beklemeden önce yakalıyor, öncülün `created_at`'i
ise daha sonraki bir transaction'a ait, fark **negatife** düşüyor ve bacak
atılıyordu. Böylece ne **değeri** ne
**satır seçimi** istemciden erişilebilir (SEÇİM katmanını kapatan budur).
**Negatif beyan bacağı düşürülür, sıfıra çekilmez** (İŞARET katmanı): düşürmek
saldırgana karşı fail-closed'dır, sıfıra çekmek ise tek bir ileri tarihli satırla
o kişinin **her gerçek tap'ini** 72 saate kadar `ignored` yapardı.
**Manuel istisnası sorgunun yükleminde**: manuel satırda `created_at` müdürün
yazdığı andır (çağıran için kaçış yolu değil — `channel` sunucu türetimli ve
`manual` ayrıca `entered_by` ister). **Verdict'e göre filtreleme yok**, bilinçli:
reddedilmiş bir tap de fiziksel bir dokunuştur, ve filtrelemek bacağı boş tutmanın
yolu olurdu.

**DÖRDÜNCÜ KATMAN — EŞZAMANLILIK.** `gather` ve `write` ayrı transaction'lardı →
aynı anda gelen N istek hiçbiri yazmamışken okur → **50 eşzamanlı POST = 51
sayılan** (2 eşzamanlı bile yetiyor; aynı şekil practice'i de çoğaltıyordu: 20/20
`practice=true`). Çözüm: `gather`+`Decide`+`write` **tek transaction** ve ilk
ifadesi kişi başına **`pg_advisory_xact_lock`**.

**Kural sonrası aynı sondalar:** SEÇİM → **1 sayılan + 19 `ignored`** · İŞARET →
**0 sayılan, 20'si de `sys:person-debounce`** · EŞZAMANLILIK → **50 satır, 1
sayılan, 49 `ignored`**; practice **20 → 1**; 30 farklı kişi eşzamanlı → 0
non-200 · NFC kontrolü değişmedi.
**Migration gerekmedi** (`created_at` migration 00005'ten beri var).

⚠️ **Açık bedel:** çevrimdışı kuyruk (M9-01) aynı senkronda saniyeler arayla iki
tap gönderirse ikincisi `ignored` olur. ADR 0006 "Sonuçlar" md. 1 — M9-01 kuyruk
boşaltmasını sunucunun doğrulayabileceği bir işarete bağlamak zorunda.

## Etiket yaşam döngüsü

`active` → normal · `retired` → "Replace tag" sonrası eski UID; tap → reject ·
`lost` → bildirilmiş kayıp; tap → reject + güvenlik uyarısı.

Değişimde eski satır **silinmez**: `retired_at` damgalanır, geçmiş işlemler
`tag_uid` üzerinden hâlâ çözülebilir (audit). Yeni UID yeni satırdır.

## Test vektörleri — zorunlu

`test/fixtures/sun_vectors.json` içinde, **sahte** anahtarlarla. Her biri bir test:

| Vaka | Beklenen |
|---|---|
| geçerli CMAC, ctr = last_ctr + 1 | kabul, last_ctr güncellenir |
| geçerli CMAC, ctr = last_ctr (tekrar) | **reject** — replay |
| geçerli CMAC, ctr < last_ctr | **reject** — geriye sayaç |
| geçerli CMAC, ctr = last_ctr + 5000 | kabul + flag (şüpheli sıçrama) |
| yanlış anahtarla üretilmiş CMAC | **reject** |
| tek bit bozulmuş CMAC | **reject** |
| eksik/bozuk hex, yanlış uzunluk | **reject**, panic yok |
| bilinmeyen UID | **reject** |
| retired / lost etiket | **reject** |
| N goroutine aynı (uid, ctr) ile yarışır | tam **1** başarı (`-race` ile) |

Son satır pazarlıksızdır: replay korumasının tek gerçek kanıtı odur.

## Go uygulama notları

- CMAC: `crypto/aes` + RFC 4493 CMAC. Küçük ve iyi test edilmiş bir uygulama
  yaz (`internal/sun/cmac.go`) veya `github.com/aead/cmac` gibi tek amaçlı bir
  paket kullan. Genel amaçlı kripto framework'ü ekleme.
- Kriptografi **yalnızca** `internal/sun` içinde. Handler CMAC hesaplamaz;
  `sun.Verify(ctx, params) (Result, error)` çağırır.
- `sun.Verify` DB'ye yalnızca sayaç ilerletmesi için dokunur; karar verme
  `internal/domain/tap`'in işidir. Sorumluluk karıştırma.
- Hata mesajları kullanıcıya jenerik ("Couldn't verify that tap — try again"),
  log'a ayrıntılı ama **anahtarsız/CMAC'siz**.

## Donanım / tedarik

Geliştirme için 10'luk NTAG 424 DNA etiket (~30 €) + **NXP TagWriter** (Android)
ile SDM mirroring encode edilir. Encode ederken: UID mirror + counter mirror açık,
SDM MAC input offset ayarlı, dosya okuma izni herkese açık, yazma izni anahtarla
kilitli. Ölçekte encode'lu tedarikçiden alınır — anahtarlar bize teslim edilir ve
üretimde döndürülür.

Referans: NXP **AN12196** ("NTAG 424 DNA — features and hints") ve
**NT4H2421Gx** veri sayfası, SDM bölümü.
