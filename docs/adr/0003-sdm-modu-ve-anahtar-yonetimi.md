# ADR 0003 — SDM modu ve anahtar yönetimi

- **Durum:** kabul edildi
- **Tarih:** 2026-07-26

## Bağlam

Tappa'nın *proof of moment* kanıtı NTAG 424 DNA çipinin **SDM (Secure Dynamic
Messaging)** özelliğine dayanır: çip her okutmada NDEF URL'sini kendi içinde
yeniden yazar, okuma sayacını (`ctr`) 1 artırır ve çipe gömülü AES-128 anahtarla
bir CMAC üretir. Diğer üç kanıt (oturum, IP, GPS) taklit edilebilir bağlamdır;
SUN edilemez (skill `tappa-sun`).

Bu ADR iki kararı normatif olarak sabitler:

1. Etiketlerin **hangi SDM moduyla encode edileceği** — `ctr` ve `cmac`'in URL'de
   nasıl göründüğü, MAC'in neyin üzerinden hesaplandığı.
2. Anahtarların **nasıl üretilip saklanacağı** — üretim, sarmalama (envelope
   encryption), döndürme ve 24-bit sayaç sarması.

Bu karar hem doğrulama kodunu (`internal/sun`, [M2-03](../plan/m2-sun.md)/M2-04/
M2-05) hem de **fiziksel etiket encode sürecini** ([M8-05](../plan/m8-deploy-pilot.md))
belirler. Etiketler yanlış modda veya yanlış anahtar stratejisiyle encode edilirse
geri dönüş sahada plaket değişimi demektir — bu yüzden karar koda değil, önce
buraya yazılır. Sıfır hafızalı bir ajan bu ADR'den **tek yorum** çıkararak M2-03
(URL ayrıştırma) ve M2-04 (session key + kısaltılmış MAC) yazabilmelidir.

İki girdi karara bağlanmıştı ve bu ADR onları uygular:

- **Q05 — SDM mirroring modu: plain** ([open-questions.md](../plan/open-questions.md)).
- **Q06 — etiket anahtar stratejisi: plaket-başına rastgele** (aynı yer).

Şema tarafı da hazır: `tags` tablosu (M1-05, `db/migrations/00004_create_tags.sql`)
zaten `uid char(14)` (7-byte UID'nin hex metni), `aes_key_ref bytea` (KEK ile
sarmalanmış anahtar) ve `last_ctr integer` (son görülen sayaç) taşır. Bu ADR o
şemayla birebir tutarlı olmak zorundadır.

Referans donanım belgeleri: NXP **AN12196** ("NTAG 424 DNA and NTAG 424 DNA
TagTamper features and hints") — SDM bölümü — ve **NT4H2421Gx** veri sayfası.
Aşağıdaki SV2 sabiti, kısaltma indeksleri ve MAC girdisi bu iki belgeye dayanır.

## Karar

### 1. Mirroring modu: PLAIN (Q05)

Etiketler **plain SDM mirroring** ile encode edilir: tap URL'si UID ve sayacı
**açık** (şifresiz) taşır. Şifreli PICC data modu **terk edildi** (bkz.
Değerlendirilen alternatifler).

URL biçimi normatiftir:

```
https://<host>/t?tag=<uid>&ctr=<ctr>&cmac=<mac>
```

| Parametre | Tip | Uzunluk | Anlam |
|---|---|---|---|
| `tag` | UID hex | **7 byte → 14 hane** hex | çip kimliği; `tags.uid` ile birebir |
| `ctr` | sayaç hex | **3 byte → 6 hane** hex, **big-endian** | okuma sayacı, her okutmada +1 |
| `cmac` | kısaltılmış CMAC hex | **8 byte → 16 hane** hex | SDM MAC'inin kısaltılmış hali |

- Parametre adları **tam olarak** `tag`, `ctr`, `cmac`'tir. M2-03 bu üç adı tek
  yerde sabit tanımlar; encode süreci (M8-05) aynı adları mirror eder.
- `ctr` **big-endian** okunur. Little-endian okumak sessizce yanlış ama "makul"
  sayılar üretir (skill `tappa-sun` tuzağı) — bu yüzden byte sırası burada
  normatiftir, yoruma bırakılmaz.
- QR kanalında `ctr` ve `cmac` **yoktur** (QR statiktir, SUN taşımaz); bu bir
  hata değil geçerli bir durumdur → `sun_valid = false`, karar `flag` yolundan
  (baseline `base:qr-requires-ip`, Q15). M2-03 bu ayrımı tipte açıkça gösterir.

### 2. MAC girdisi: BOŞ (SDMMACInputOffset yok)

Plain SUN URL'sinde MAC'e giren **ek mirror'lanmış dosya verisi yoktur**
(SDMMACInputOffset ile SDMMACOffset çakışır → sıfır bayt girdi). Dolayısıyla SDM
MAC'i **boş mesaj** üzerinden hesaplanır:

```
full = AES-CMAC(K_session, "")      // sdmMacInput boş
```

Bu, AN12196'nın "yalnız UID + counter mirroring" yapılandırmasının doğal
sonucudur ve bilinçli bir karardır:

- **Tazelik ve kimlik MAC girdisinden değil, session key türetiminden gelir.**
  `K_session = CMAC(K_sdmfileread, SV2)` ve `SV2` içinde **hem UID hem `ctr`**
  vardır (madde 6). Boş mesaj üzerinden hesaplanan CMAC bile o özgün
  `(UID, ctr)` çifti için anahtar sahipliğini kanıtlar — çünkü session key o
  çiftten türetilir. UID/ctr'yi ayrıca MAC mesajına koymak fazlalık olurdu.
- M2-04 adım 3'ü (`full = CMAC(K_session, sdmMacInput)`) bununla **tutarlıdır**:
  `sdmMacInput` bu modda boş bir dilimdir (`[]byte{}` / `nil`), sıfır uzunluklu
  bir CMAC girişi. Bu bir "eksik" değil, encode kararının doğrudan karşılığıdır.

> Bir sonraki geliştiricinin en olası hatası boş mesajı bir bug sanıp UID/ctr'yi
> MAC girdisine eklemeye çalışmaktır. **Eklemeyin.** Encode SDMMACInput'suz
> yapıldığı için çip de boş mesajın CMAC'ini yayar; girdi eklerseniz doğrulama
> *her zaman* başarısız olur ve hata "anahtar yanlış" gibi görünür.

### 3. Anahtar üretimi: plaket-başına RASTGELE (Q06)

Her plaket için **bağımsız, rastgele üretilmiş AES-128 anahtarı** (K_sdmfileread)
kullanılır. Master anahtardan UID ile **türetme terk edildi** (bkz. Değerlendirilen
alternatifler).

- Anahtar `crypto/rand` ile üretilen 16 baytlık rastgele değerdir; hiçbir başka
  anahtardan/UID'den türetilmez.
- İzolasyon garantisi: bir plaketin anahtarı sızarsa **yalnız o plaket** düşer.
  Park geneli bir sır yoktur; tek-nokta felaketi mümkün değildir (§4.7 ruhu).
- Plain modla (madde 1) tam uyumlu: plain SDM UID'yi açık taşıdığı için
  çözümlemede paylaşılan bir meta-read anahtarına gerek yoktur; her tag'in kendi
  anahtarı yeterlidir.

### 4. Anahtar sarmalama: AES-256-GCM envelope encryption

Düz (plaintext) etiket anahtarı **DB'de, log'da, hata mesajında veya repoda ASLA**
bulunmaz (§4.7). Her etiket anahtarı bir Key-Encryption-Key ile sarmalanıp
`tags.aes_key_ref` (bytea) içinde saklanır:

- **KEK:** `TAPPA_TAG_KEK` — **32 baytlık** (AES-256) bir anahtar, ortam
  değişkeninden okunur (env kodlaması — hex/base64 — ve `internal/config`
  doğrulaması M2-05'in işidir; repoda yer almaz).
- **Algoritma:** AES-256-GCM (`crypto/aes` + `crypto/cipher`, yeni bağımlılık
  yok — §1). Nonce her sarmalamada `crypto/rand` ile **yeni** üretilir; asla
  yeniden kullanılmaz. Tek uzun ömürlü KEK altında rastgele 96-bit nonce, park
  boyutu (binlerce tag) GCM doğum-günü sınırının çok altında kaldığı için
  güvenlidir.
- **AAD (ek doğrulanmış veri): tag UID — v1'de ZORUNLU.** Sarmalama ve açma, ek
  doğrulanmış veri olarak etiketin **ham 7-byte UID'sini** alır:
  `Wrap(uid, key) → gcm.Seal(nonce, nonce, key, aad)` ve
  `Unwrap(uid, ref) → gcm.Open(dst, nonce, ciphertext||gcm_tag, aad)`; her
  ikisinde de `aad = ham 7-byte UID` — yani 14-hex `tags.uid`'nin hex-decode
  edilmiş hâli. **Neden ham 7-byte, 14-hex metin değil:** `tags.uid` CHECK'i
  büyük/küçük harfin ikisini de kabul eder (kanonikleştirme uygulama katmanının
  işidir, M1-05); hex metnini AAD yapmak AAD'yi harf durumuna duyarlı kılardı ve
  aynı tag "91ac…" ile sarılıp "91AC…" ile açılamazdı. Ham bayt kanoniktir ve
  zaten SV2'de (madde 6) kullanılan biçimdir → doğrulama yolunda elde hazırdır.

**Neden AAD=UID v1'de zorunlu, ertelenmez.** `tappa_app` rolü `tags` üzerinde
`UPDATE` yetkisine sahiptir (`last_ctr` ilerletmesi için — M1-05). AAD olmadan
GCM yalnız nonce + ciphertext'i doğrular, satırın UID'sini **değil**; dolayısıyla
bir tag'in sarmalı `aes_key_ref`'i başka bir tag satırına **taşınabilir** ve
Unwrap yine başarılı olur — anahtar yanlış plakete bağlanır. AAD=UID bunu unwrap
anında **kimlik doğrulama hatasıyla** yakalar: sarmalı anahtar tag'ine
**bağlıdır**, satırlar arası taşınamaz. Bu, şemanın her yerindeki kuşak+kemer
(bileşik FK ile çapraz-tenant bağlanmayı **yapısal** engelleme — M1-05) ethosuyla
aynı çizgidedir. Maliyeti **sıfır**: henüz hiçbir tag sarılmadı (pre-production),
yani AAD eklemek `gcm.Seal/Open`'a tek argümandır. Ertelemek ise sahada **tüm
parkı yeniden sarmalama** borcu yaratırdı — yanlış yön.

**`aes_key_ref` byte düzeni — NORMATİF.** M2-05 Wrap/Unwrap bunu birebir uygular:

```
aes_key_ref = nonce (12 byte) || ciphertext (16 byte) || gcm_tag (16 byte)
            = toplam 44 byte
```

| Ofset | Uzunluk | İçerik |
|---|---|---|
| 0–11  | 12 byte | GCM nonce (rastgele, sarmalama başına yeni) |
| 12–27 | 16 byte | sarmalanmış AES-128 anahtarın şifreli metni |
| 28–43 | 16 byte | GCM kimlik doğrulama etiketi |

- **AAD byte düzenini DEĞİŞTİRMEZ.** AAD (ham 7-byte UID) yalnız GCM kimlik
  doğrulamasına girer; sarmalı blob'a **yazılmaz**. Bu yüzden düzen ve **sabit
  44 baytlık** uzunluk AAD'den etkilenmez — `aes_key_ref` yine tam
  `nonce(12) || ciphertext(16) || gcm_tag(16)`'tir.
- Bu düzen Go'nun standart deyimiyle **doğal** üretilir:
  `gcm.Seal(nonce, nonce, key16, aad)` çıktısı `nonce || ciphertext || tag`
  şeklindedir (AAD çıktı baytlarına eklenmez); ayrı bir birleştirme/ayrıştırma
  mantığı gerekmez.
- Ciphertext, 16 baytlık AES-128 anahtarı sarmaladığı için **tam 16 bayttır**
  (GCM akış şifresidir, padding yok). Bu yüzden `aes_key_ref` uzunluğu **sabit
  44 bayttır**; Unwrap girişte uzunluğu doğrular, 44 değilse KEK'i hiç
  çağırmadan hata döner (panik değil).
- **Unwrap** `uid`'yi ve `ref`'i alır: ilk 12 baytı nonce, kalan 32 baytı
  `ciphertext || tag` olarak ayırıp `aad = ham 7-byte UID` ile `gcm.Open`
  çağırır. Yanlış KEK, **yanlış UID (AAD uyuşmazlığı)**, bozuk `aes_key_ref`
  veya oynanmış tag → GCM kimlik doğrulama hatası → **hata döner, panik yok**.
  Açılan düz anahtar yalnız doğrulama süresince bellekte yaşar ve kullanım
  sonrası sıfırlanır (`clear()` — ayrıntı M2-05).

### 4b. 24-bit sayaç sarması (ctr wrap-around)

`ctr` 24 bittir → **16.777.215'te (0xFFFFFF) sarar**, sonraki okutmada 0'a döner.
`last_ctr` DB'de `integer` (32-bit signed, tavan 2.147.483.647) sütununda tutulur;
24-bit tavan buraya rahatça sığar, sütun taşması olmaz.

**Sarma anındaki davranış — normatif:**

- Replay koruması **strict monoton** `WHERE last_ctr < @ctr` ifadesidir (§4.4,
  M2-06). Sarma gerçekleşirse `ctr` 0'a döner, `last_ctr` ise 16.777.215'tir →
  atomik UPDATE **0 satır** döndürür → tap **reject** edilir (replay'den ayırt
  edilemez). Bu **fail-closed**tur: yanlış yön güvenli yöndür — geçersiz kabul
  değil, geçerli reddetme.
- **Monoton kontrol sarmayı akomode etmek için ASLA gevşetilmez.** "Sarma
  olabilir, bu yüzden küçük `ctr`'yi kabul et" biçimindeki her istisna, tam da
  §4.4'ün kapattığı replay penceresini yeniden açar. `internal/sun` içine sarma
  özel-durum mantığı **eklenmez**; kontrol strict kalır.
- Sarma bu yüzden **kodda değil, operasyonel olarak** ele alınır: `last_ctr`
  tavana yaklaşan bir etiket, tavanı geçmeden **retire + replace** edilir
  (madde 5). Pratikte gün başına 20 tap ile tavana ulaşmak ~2000 yıl sürer;
  yani bu bir hipotetik değil, kayıt altına alınmış bir operasyonel eşiktir —
  tavana yakın `last_ctr` bir anomali sinyalidir, meşru bir etikette görülmez.

### 5. Anahtar döndürme

- **Fabrika/tedarik durumu:** etiketler tedarikçiden/fabrikadan **varsayılan**
  anahtarla gelir (NXP fabrika varsayılanı veya tedarikçi provizyonu). Bu
  varsayılan anahtar üretimde **asla** kullanılmaz.
- **Encode süreci** ([M8-05](../plan/m8-deploy-pilot.md)): her tag'e Tappa'nın
  **plaket-başına rastgele** anahtarı (madde 3) yazılır (K_sdmfileread
  varsayılandan değiştirilir), plain SDM yapılandırması (UID + counter mirror,
  SDMMACInput'suz — madde 1/2) kurulur, üretilen anahtar KEK ile sarmalanıp
  (madde 4) `aes_key_ref`'e yazılır. Düz anahtar bu adımdan sonra hiçbir yerde
  kalıcılaşmaz.
- **Rotasyon:** park geneli bir master **olmadığı için** (madde 3) toplu
  yeniden-anahtarlama yoktur. Bir anahtar şüphesi **tekil** ele alınır: ilgili
  etiket `status='retired'` ile emekli edilir ve **yeni UID + yeni rastgele
  anahtar** taşıyan yeni bir plaketle fiziksel olarak değiştirilir (`replaced_by`
  zinciri, M1-05 tag yaşam döngüsü). Geçmiş işlemler eski `tag_uid`'den hâlâ
  çözülür (silme yok — §4.6, audit izi).
- Bu, plaket-başına rastgele stratejinin bilinçli bedelidir: ucuz toplu rotasyon
  yok; karşılığında patlama yarıçapı **tek plaket**. NFC üzerinden yerinde
  yeniden-anahtarlama (mevcut anahtarla `ChangeKey`) teknik olarak mümkündür ama
  MVP kapsamı dışıdır; normatif rotasyon yolu **retire + replace**'tir.

### 6. Algoritma detayı — atıfla, kopyalanmaz

Session key türetimi ve MAC kısaltmasının **normatif metni** skill `tappa-sun`
ve M2-04'e aittir; bu ADR yalnız özetler ki M2-03/M2-04 buradan tek yorumla
yazılabilsin:

```
SV2       = 3C C3 00 01 00 80 || UID || ctr        (NXP AN12196 §SDM)
K_session = AES-CMAC(K_sdmfileread, SV2)
full      = AES-CMAC(K_session, sdmMacInput)        // madde 2: sdmMacInput boş
mac       = full[1], full[3], full[5], … full[15]   // tek-indeksli 8 byte
karar     = subtle.ConstantTimeCompare(mac, gelen_cmac)
```

> **Ek not (2026-08-18, M2-08 — NORMATİF).** Yukarıdaki `SV2` satırındaki `ctr`
> **LSB-first** yazılır; URL'deki `ctr` ise (madde 1) **MSB-first**'tür. İkisi aynı
> değerin iki farklı yazılışıdır ve **birbirinin bayt-tersidir**. Bu satır orijinal
> hâlinde bayt sırasını söylemiyordu; karar değişmedi, **eksik olan sıra eklendi**.
>
> Kaynak — NXP **AN12196 rev. 1.8, §4.3 Tablo 2, s. 10** (rev. 2.0'da §3.3 **Tablo
> 1**, s. 9 — tablo iki revizyon arasında yalnız taşınmadı, **yeniden numaralandı**;
> revizyonu belirtmeden "Tablo 2" demek yanlış tabloyu gösterir), UID
> `04C767F2066180` için:
>
> - adım 4: `SDMReadCtr = 010000` — belgenin kendi notu: *"(LSB first as per
>   [Section 3.1])"*
> - adım 7: `SV2 = 3CC3 0001 0080 04C767F2066180 010000`
>
> ve aynı UID için **§4.4.1, s. 11** düz SUN URL örneği:
> `…?uid=04C767F2066180&ctr=000001&c=…` — yani URL'de `000001`, SV2'de `010000`.
>
> 🔴 **Bir sonraki geliştiricinin İKİNCİ en olası hatası** (madde 2'deki boş-mesaj
> tuzağıyla aynı sınıfta): URL'den gelen 3 baytı SV2'ye **olduğu gibi** eklemek.
> Bu tam olarak yapıldı ve aylarca fark edilmedi — `internal/sun/verify_mac.go`
> içindeki `sv2()` baytları verbatim ekliyordu ve bunu bir *yapısal garanti* diye
> yorumluyordu ("çip URL'ye SV2'ye verdiği sırayla yazar"). **Yanlıştır.**
>
> Neden görünmedi: 3 baytlık sayaçların yalnız **1/256**'sı palindromdur ve
> **1–255 aralığında hiç palindrom yoktur**, yani gerçek bir çipin **ilk 255
> tap'inin tamamı** reddedilirdi. Test paketi yeşildi çünkü tüm vektörler aynı
> hatalı `sv2()` ile üretiliyordu (`test/fixtures/sun_vectors.json` bunu kendi
> uyarısında itiraf ediyordu). Kusur **sevk edildi ama tetiklenemezdi** — ve bunun
> gerekçesi bir satır sayısı değil, bir **yokluk zinciri**: M8-05 encode runbook'u
> FAZ B'de duruyor, encode aracı **hiç yazılmadı** (`cmd/` altında yok; `internal`
> içinde `AuthenticateEV2First`/`ChangeFileSettings`/`ChangeKey` uygulayan tek satır
> yok) ve panelin plaket yaratma yolu da yok (`db/queries/tags.sql` **hiç INSERT
> taşımıyor**, bunu kendi yorumunda *karar* olarak söylüyor). Encode edilmiş tek bir
> plaket yoksa gerçek bir tap da olamaz.
>
> 🔴 **DÖRDÜNCÜ HALKA — VE ZİNCİRİ KENDİ BAŞINA DOĞRULAYAN OKUR ONU MUTLAKA BULUR.**
> Yukarıdaki üç halka *"`tags` tablosuna satır yazan bir yol yok"* **demiyor** ve
> öyle okunmamalı: böyle bir yol **vardır**. `test/fixtures/seed.sql` doğrudan
> `INSERT INTO tags` yapar, `scripts/seed.sh`'ın ikinci yarısı (`test/fixtures/
> seedkeys`) o satırların `aes_key_ref`'ini **operatörün gerçek `TAPPA_TAG_KEK`'i
> ile** sarmalar, ve yerelde bu tablo bugün altı haneli bir satır sayısı taşır. Bu
> dördüncü yol yazılmadığı sürece zincir eksik görünür ve okur ona güvenmekte
> haklı olarak tereddüt eder. **Sonuç yine de ayakta** — üç ölçülmüş sebeple:
>
> 1. **Sarmalanan düz anahtar SAHTEDİR.** `seed.sql`'in yazdığı `aes_key_ref` düz
>    bir `PLACEHOLDER-UNTIL-seedkeys-WRAPS-IT-<uid>` metnidir; `seedkeys` onun
>    yerine `fixtures.SeedTagKey(uid)` değerlerini sarmalar ve bunlar **public,
>    bilerek gürültülü bir etiketten türetilir** (dosyanın kendi ifadesi: *"makes
>    these keys self-evidently fake"*). Gerçek bir çipin `K_SDMFileRead`'i hiçbir
>    aşamada ortada yoktur. Sarmalayan KEK gerçektir, **sarmalanan şey değildir**.
> 2. **Yol yalnız YEREL.** `scripts/seed.sh` psql'i `docker compose exec -T db` ile
>    çağırır — üretim veritabanına giden bir kolu yoktur.
> 3. **Ve asıl sebep: bir SATIR bir PLAKET DEĞİLDİR.** Geçerli bir SUN, URL'yi
>    imzalayan **fiziksel bir çip** ister (madde 6'daki CMAC'i o hesaplar).
>    `tags`'taki satır yalnızca *"bu UID için bu anahtarı bekliyoruz"* der; duvarda
>    o UID'yi taşıyan bir çip yokken hiçbir tap doğrulanamaz — bu satırlar sayacı
>    bile ilerletemez.
>
> Yani ilk üç halka **encode aracının yokluğunu** kanıtlar; dördüncüsü de
> *"öyleyse veritabanındaki satırlar ne?"* sorusunu kapatır.
>
> ⚠️ **Bunu `tags` satır sayısıyla ölçmeye kalkma** — o ölçüm yanıltır ve nedeni
> M2-08'de ölçüldü: `tappa_app` rolüyle `select count(*) from tags` **0** döner,
> `tappa_owner` rolüyle aynı sorgu **altı haneli** bir sayı döner; fark RLS'tir
> (`app.tenant_id` kurulmamış bir bağlantı hiçbir satır görmez, tablo doluyken bile).
> Yani boş bir tablo ile dolu bir tablo, uygulama rolünden **aynı görünür**.
> Ayrıntı: [m2-sun.md](../plan/m2-sun.md) → M2-08 tuzakları.
>
> Bunun tekrarını engelleyen şey artık **dış kaynaklı** known-answer vektörleridir:
> `internal/sun/an12196_kat_test.go` beklenen değerleri AN12196'nın yayımladığı
> tablolardan alır, bizim zincirimizden değil (§4.4.4.2.1 Tablo 5, s. 15 — SV2
> **düzeni**, oturum anahtarı, **boş girdi**, tek-indeksli kısaltma).
> ⚠️ **URL bayt sırası bu tabloya dayanmaz:** Tablo 5 şifreli-PICCData örneğidir,
> yayımlanan URL'i `?e=…&c=…` biçimindedir ve düz `ctr=` taşımaz. O eksenin **tek**
> çapası yukarıdaki Tablo 2 + §4.4.1 aynı-UID eşleşmesidir.
> **O dosyadaki beklenen değerler bizim koda göre güncellenmez**; ters yönde
> çalışır.
>
> **Değer ekseni de bu notla kapanır:** URL metni MSB-first olduğu için
> `params.go`'daki `beUint24` (big-endian) **doğrudur** — madde 1'in "big-endian"
> ifadesi URL için geçerlidir ve değişmemiştir. Bağımsız teyit: `icedevml/sdm-backend`
> → `validate_plain_sun` URL baytlarını SV2 için **ters çevirir**, değeri ise
> `'>I'` (big-endian) okur.
>
> **M8-05 encode runbook'u için sonuç:** encode tarafı sayacı URL'ye **MSB-first**
> mirror'lamak zorundadır (madde 1). Bu vektörlerin **doğrulayamadığı** tek şey
> budur — encode tarafı ancak gerçek plaketle kanıtlanır.

Kısaltma (`full` içinden **tek indeksli** baytlar) M2-04'ün en kritik ve en çok
zaman kaybettiren adımıdır: NTAG 424 tam 16 baytlık CMAC'i değil, bu 8 baytlık
kısaltılmış hali yayar (skill `tappa-sun`). Karşılaştırma `crypto/subtle.
ConstantTimeCompare` ile yapılır (`bytes.Equal`/`==` değil). Bu ADR bu adımların
**varlığını ve sırasını** sabitler; bit düzeyi uygulama ve test vektörleri
M2-04/M2-07'nindir.

## Gerekçe

- **Neden plain (Q05).** UID zaten fiziksel plakette basılıdır ve sistem onu
  **public** kabul eder (`resolve_tag_by_uid` UID ile bağlamsız sorgulanır, UID
  tap URL'sinde açıktır — ADR 0002 madde 7, M1-05). Şifreli PICC'in mahremiyet
  kazancı bu yüzden düşüktür; buna karşılık PICC'i çözmek **paylaşılan bir
  meta-read anahtarı** gerektirir (UID bilinmeden çözülemez) → bu, Q06'nın
  "plaket-başına rastgele" kararıyla doğrudan çelişir ve park geneli tek-nokta
  risk yaratır. Plain mod hem daha basit ayrıştırma sağlar hem de paylaşılan
  anahtar gerektirmez; güvenlik CMAC (proof of moment) ve monoton `ctr` (replay)
  'den gelir, gizlilikten değil.
- **Neden boş MAC girdisi.** Tazelik ve kimlik bağlaması SV2 üzerinden (UID+ctr
  session key'e girer) sağlandığı için MAC mesajına ayrıca veri koymak fazlalık
  olurdu; encode'u sadeleştirir ve M2-04'ü tek yoruma indirger. AN12196 bu
  yapılandırmayı (mirror var, MAC input yok) açıkça destekler.
- **Neden plaket-başına rastgele (Q06).** İzolasyon: bir anahtar sızarsa yalnız
  o plaket düşer. Master-türetme encode'u kolaylaştırırdı ama **master sızarsa
  tüm park düşer** — bir güvenlik ürünü için kabul edilemez tek-nokta risk.
  Şema zaten per-tag `aes_key_ref` taşıdığı için per-tag anahtarın depolama
  maliyeti sıfırdır.
- **Neden AES-256-GCM sarmalama.** GCM hem gizlilik hem bütünlük (kimlik
  doğrulama etiketi) verir: yanlış KEK veya oynanmış `aes_key_ref` sessizce
  çöp anahtar üretmez, **doğrulama hatası** verir. AAD=UID (madde 4) ile sarmalı
  anahtar tag'ine bağlanır → `tappa_app`'in `UPDATE` yetkisiyle bir satırın
  `aes_key_ref`'ini başka satıra taşıması unwrap'ta yakalanır. `crypto/aes`+
  `crypto/cipher` stdlib'dedir → yeni bağımlılık yok (§1). Sabit 44 baytlık
  düzen, `gcm.Seal`'in doğal çıktısıyla birebir örtüşür → M2-05 ek serileştirme
  kodu yazmaz.
- **Neden sarmada retire, koda özel-durum değil.** Monoton kontrolü sarma için
  gevşetmek §4.4 replay penceresini yeniden açar. Sarma pratikte ~2000 yıl
  uzakta olduğundan doğru maliyet dengesi, kontrolü strict bırakıp nadir sarmayı
  operasyonel bir retire/replace ile karşılamaktır — kod sadeliği ve replay
  güvenliği korunur.

## Sonuçlar

- **[M2-03](../plan/m2-sun.md) (URL ayrıştırma):** parametre adları `tag`/`ctr`/
  `cmac`; `tag` 14 hex, `ctr` 6 hex **big-endian**, `cmac` 16 hex. QR'da
  `ctr`/`cmac` yokluğu geçerli durum (`sun_valid=false`). Bu ADR tek yetkili
  kaynaktır; adlar tek yerde sabitlenir.
- **[M2-04](../plan/m2-sun.md) (session key + kısaltılmış MAC):** madde 6'daki
  beş satır birebir uygulanır; `sdmMacInput` boş, kısaltma tek-indeksli 8 byte,
  karşılaştırma `subtle.ConstantTimeCompare`.
- **[M2-05](../plan/m2-sun.md) (Wrap/Unwrap):** AES-256-GCM, `TAPPA_TAG_KEK`
  (32 byte); `Wrap(uid, key)` ve `Unwrap(uid, ref)` **uid parametresi alır** ve
  `aad = ham 7-byte UID` verir. `aes_key_ref = nonce(12)||ciphertext(16)||
  gcm_tag(16)` = 44 byte (AAD blob'a yazılmaz, düzeni değiştirmez); Unwrap
  uzunluğu doğrular, yanlış KEK **veya yanlış UID**'de kimlik doğrulama hatası
  (panik değil), açılan anahtar kullanım sonrası sıfırlanır.
- **[M2-06](../plan/m2-sun.md) (atomik sayaç):** `WHERE last_ctr < @ctr` strict
  kalır; sarma için istisna eklenmez. Sarmaya yaklaşan tag operasyonel olarak
  retire/replace edilir.
- **[M8-05](../plan/m8-deploy-pilot.md) (encode runbook):** her tag için rastgele
  AES-128 üret → plain SDM (UID+counter mirror, MAC input yok) yapılandır →
  anahtarı KEK ile sarmala → `aes_key_ref`'e yaz. Fabrika varsayılan anahtarı
  daima değiştirilir.
- **Q05 ve Q06** `open-questions.md`'de zaten "cevaplandı" işaretli (orkestratör
  işledi); bu ADR onları normatif metne dönüştürür.
- **`aes_key_ref` byte düzeni ve AAD=UID kararı** (madde 4) tek yerde
  sabitlendi; ileride AAD şemasını (ör. UID yerine başka bağlam) veya farklı bir
  KDF/algoritma değiştirmek **yeni bir ADR** ve tüm parkın yeniden sarmalanmasını
  gerektirir — düzen ve AAD sessizce değiştirilemez.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| **Şifreli PICC data mirroring** (UID + ctr şifreli, `?picc=…`) | Mahremiyet kazancı düşük çünkü UID zaten public (plakette basılı, çözümleme yolu onu public kabul eder — ADR 0002 madde 7). Çözmek **paylaşılan meta-read anahtarı** gerektirir → Q06'nın per-tag rastgele kararıyla çelişir, park geneli tek-nokta risk. Plain mod aynı güvenliği (CMAC + monoton ctr) paylaşılan sır olmadan verir. (Q05) |
| **Master anahtardan UID-türetilmiş etiket anahtarı** (`K_tag = KDF(master, UID)`) | Encode'u ve rotasyonu kolaylaştırır ama **master sızarsa tüm park düşer** — bir güvenlik ürünü için kabul edilemez tek-nokta felaketi. Per-tag rastgele ile patlama yarıçapı tek plakete iner; depolama maliyeti şema zaten `aes_key_ref` taşıdığı için sıfır. (Q06) |
| **MAC girdisine UID/ctr eklemek** (SDMMACInput dolu) | Fazlalık: UID+ctr zaten SV2 üzerinden session key'e girer. Encode'u karmaşıklaştırır ve M2-04'ü çok-yoruma açar; boş girdi hem AN12196 uyumlu hem tek yorumlu. |
| **Sarma için monoton kontrolü gevşetmek** (küçük `ctr`'yi "sarma olabilir" diye kabul) | §4.4 replay penceresini yeniden açar. Sarma ~2000 yıl uzakta; doğru denge kontrolü strict bırakıp nadir sarmayı retire/replace ile karşılamaktır. |
| **Anahtar sarmalamada AES-CBC/CTR (kimlik doğrulamasız)** | Bütünlük vermez: oynanmış veya yanlış-KEK'le açılmış `aes_key_ref` sessizce çöp anahtar üretir, hata SUN doğrulamasına kadar gizlenir. GCM kimlik doğrulama etiketiyle hata sarmalama anında yakalanır. |
| **Düz anahtarı bellekte/struct'ta cache'lemek** (performans için) | Bellek dökümü tüm parkı verir (§4.7). Anahtar yalnız doğrulama süresince açılır ve sıfırlanır; gerekirse ayrı bir ADR ile tartışılır (M2-05 tuzağı). |
