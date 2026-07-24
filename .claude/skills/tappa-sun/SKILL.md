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
   SV2 = 3C C3 00 01 00 80 || UID || ctr   (NXP AN12196 §SDM)
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

## QR fallback — SUN yok

Plakette NFC ile birlikte QR da basılır (NFC'siz/kapalı telefonlar için).
QR statiktir → `ctr` ve `cmac` yoktur → `sun_valid = false`.
Bu kanalda sunucu **IP eşleşmesini zorunlu tutar**; GPS tek başına yetmez →
karar `flag`. (Karar Q15; baseline politikası `base:qr-requires-ip`, tenant
değiştirebilir. Gerekçe: QR fotoğraflanır, süresiz geçerlidir ve hiçbir fiziksel
dokunuş kanıtı taşımaz — sahte konumla evden APPROVED alınabilirdi.)
QR asla NFC ile aynı güven seviyesine çıkarılmaz. `channel` alanı bunu ayırt eder:
`nfc | qr | manual`.

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
