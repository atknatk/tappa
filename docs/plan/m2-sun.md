# M2 — SUN doğrulama (`internal/sun`)

**Amaç.** *Proof of moment*: bir tap URL'sinin gerçekten şu anda, gerçek çipe
fiziksel dokunuşla üretildiğini kanıtlayan katman. Diğer üç kanıt taklit
edilebilir bağlamdır; SUN edilemez.

**Bittiğinde:** `sun.Verify(ctx, params) (Result, error)` çalışıyor, replay
gerçek eşzamanlılık testiyle kapatılmış, kapsam **%90+**.

**Ana araç:** skill `tappa-sun` — bu milestone'un her görevinde önce oku.
Algoritmanın normatif metni orada.

> **Kriptografi yalnızca `internal/sun` içinde yaşar.** Handler CMAC hesaplamaz.
> `sun.Verify` DB'ye yalnızca sayaç ilerletmek için dokunur; **karar vermez** —
> karar `internal/domain/tap`'in işi.

---

## M2-01 — ADR 0003: SDM modu ve anahtar yönetimi

- **Bağımlılık:** M1-05 · Q05 · Q06
- **Kırmızı çizgi:** §4.7
- **Commit:** `docs(adr): choose SDM mirroring mode and key strategy`

**Amaç.** Etiketlerin hangi SDM moduyla encode edileceğini ve anahtarların nasıl
üretilip saklanacağını karara bağlamak.

**Neden.** Bu karar hem doğrulama kodunu hem de **fiziksel etiket encode
sürecini** (M8-05) belirler. Etiketler yanlış modda encode edilirse geri dönüş
sahada plaket değişimi demektir.

**Karara bağlanacaklar.**
1. **Mirroring modu** (Q05): plain UID + ctr mi, şifreli PICC data mı.
2. **MAC input** var mı (SDMMACInputOffset) — yoksa mesaj boştur.
3. **Anahtar üretimi** (Q06): plaket başına rastgele mi, master'dan UID ile
   türetilmiş mi.
4. **Sarmalama:** `TAPPA_TAG_KEK` ile envelope encryption; algoritma (AES-GCM)
   ve `aes_key_ref` byte düzeni.
5. **Anahtar döndürme:** üretimde tedarikçi anahtarları nasıl değişecek.
6. **ctr sarması:** 24 bit → 16.777.215'te sarar. Davranış belgelenir (günde 20
   tap ile ~2000 yıl; yine de sarma anında ne olacağı yazılı olmalı).

**Kabul kriterleri.**
- ADR yazıldı; NXP **AN12196** ve **NT4H2421Gx** veri sayfasına referans var.
- M2-03 ve M2-04 bu ADR'den tek yorum çıkararak yazılabiliyor.
- Q05 ve Q06 `open-questions.md`'de cevaplandı olarak işaretlendi.

---

## M2-02 — AES-CMAC (RFC 4493)

- **Bağımlılık:** M0-02
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(sun): add RFC 4493 AES-CMAC`

**Amaç.** Doğruluğu kanıtlanmış bir CMAC uygulaması.

**Neden.** SDM'nin tamamı bunun üstünde duruyor. Önce **bilinen cevaplarla**
CMAC'i kanıtla, sonra SDM katmanını ekle — aksi halde bir hatayı iki katmanda
birden aramak zorunda kalırsın.

**Dokunulacak dosyalar.** `internal/sun/cmac.go`, `internal/sun/cmac_test.go`

**Kabul kriterleri.**
- **RFC 4493 §4'teki dört resmi test vektörü** (boş mesaj, 16, 40, 64 byte)
  bire bir geçiyor.
- `crypto/aes` dışında kripto bağımlılığı yok.
- Subkey türetme (K1/K2) ve padding ayrı test ediliyor.
- Kapsam %90+.

**Tuzaklar.**
- Bu adımda **kısaltma yok** — burada tam 16 byte üretilir. Kısaltma SDM
  katmanının işi (M2-04).
- Genel amaçlı kripto framework'ü ekleme (CLAUDE.md §1). Tek amaçlı bir paket
  (`github.com/aead/cmac`) kabul edilebilir ama **önce sor**.

---

## M2-03 — SDM URL ayrıştırma

- **Bağımlılık:** M2-01
- **Commit:** `feat(sun): parse and validate SDM URL parameters`

**Amaç.** `https://time.tappa.mt/t?tag=…&ctr=…&cmac=…` parametrelerini güvenle
tiplere çevirmek.

**Dokunulacak dosyalar.** `internal/sun/params.go`, `params_test.go`

**Beklenen biçimler** (skill `tappa-sun`):
| Parametre | Format |
|---|---|
| `tag` / UID | 7 byte hex — 14 hane |
| `ctr` | 3 byte hex — 6 hane, big-endian |
| `cmac` | 8 byte hex — 16 hane |

**Kabul kriterleri.**
- Eksik, fazla uzun, kısa, hex olmayan, büyük/küçük harf karışık girdiler
  **hatayla** döner — panik yok.
- Ayrıştırma hatası kullanıcıya jenerik mesaj; log'a ayrıntı ama **CMAC'siz**.
- QR kanalı: `ctr`/`cmac` yokluğu geçerli bir durumdur → `sun_valid = false`,
  hata değil. Bu ayrım tipte açıkça görünür.
- Fuzz testi (`go test -fuzz`) en az kısa bir tur koşturulmuş, panik yok.

**Tuzaklar.**
- `ctr` big-endian; little-endian okumak sessizce yanlış ama "makul" sayılar üretir.
- Parametre adları encode ayarına bağlı (`tag` vs `uid` vs `picc`); ADR 0003 ne
  diyorsa o. Tek yerde sabit tanımla.

---

## M2-04 — Oturum anahtarı, kısaltılmış MAC, sabit zamanlı karşılaştırma

- **Bağımlılık:** M2-02 · M2-03
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(sun): derive session key and verify truncated CMAC`

**Amaç.** Çipin yaydığı imzayı yeniden üretip karşılaştırmak.

**Algoritma** (skill `tappa-sun`, NXP AN12196 §SDM):
1. `SV2 = 3C C3 00 01 00 80 || UID || ctr`
2. `K_session = CMAC(K_sdmfileread, SV2)`
3. `full = CMAC(K_session, sdmMacInput)` — MAC input yoksa mesaj boş
4. `mac = full[1], full[3], full[5], … full[15]` → **8 byte, tek indeksli**
5. `subtle.ConstantTimeCompare(mac, gelenCmac)`

**Kabul kriterleri.**
- Kısaltma **adım 5'e göre** yapılıyor; tam 16 byte ile karşılaştırma yok.
- Karşılaştırma `crypto/subtle.ConstantTimeCompare` — `bytes.Equal` veya `==`
  değil (`redline-check.sh` R7 bunu tarar).
- Yanlış anahtarla üretilmiş CMAC, tek bit bozulmuş CMAC → reject.
- Kapsam %90+.

**Tuzaklar.**
- **Adım 4 en çok zaman kaybettiren noktadır.** NTAG 424 tam CMAC'i yaymaz;
  tek indeksli 8 byte'ı yayar. Tam CMAC ile karşılaştıran uygulama *her zaman*
  başarısız olur ve hata "anahtar yanlış" gibi görünür.
- Timing sızıntısı teorik değil: CMAC karşılaştırması byte byte sızdırırsa
  saldırgan geçerli imza üretebilir.

> **Kart düzeltmesi (2026-08-18, M2-08 uygulaması sırasında).** Yukarıdaki
> **adım 1 eksikti** ve bu eksiklik bir kusura dönüştü. `SV2 = 3C C3 00 01 00 80
> || UID || ctr` satırı `ctr`'nin **hangi bayt sırasıyla** yazılacağını
> söylemiyor; M2-03 ise URL'deki `ctr`'yi (doğru biçimde) **big-endian** olarak
> sabitliyor. Uygulama bu boşluğu URL baytlarını SV2'ye **verbatim** koyarak
> doldurdu — yanlış yönde.
>
> **Doğrusu:** SV2'ye giren sayaç **LSB-first**'tür, URL'deki ise MSB-first —
> yani `sv2()` baytları **tam bir kez ters çevirmek zorundadır**. Kaynak:
> AN12196 rev. 1.8 §4.3 Tablo 2 s. 10 (adım 4 `SDMReadCtr = 010000` *"LSB first"*,
> adım 7 SV2) ile aynı UID'nin §4.4.1 s. 11'deki URL'si (`ctr=000001`).
>
> Adım 1 artık şöyle okunmalı:
> `SV2 = 3C C3 00 01 00 80 || UID || reverse(ctr_url)`.
> Normatif metin: [ADR 0003 madde 6 ek notu](../adr/0003-sdm-modu-ve-anahtar-yonetimi.md).
>
> **Kabul kriteri "Kapsam %90+" bu kusuru yakalayamazdı** — kapsam ölçüldüğünde
> %97 idi ve satır yine de yanlıştı. Kapsam, *doğruluğu* değil *çalıştırılmışlığı*
> ölçer; bu satırı ancak **dış kaynaklı** bir vektör yakalayabilirdi (M2-08).

---

## M2-05 — Anahtar sarmalama (KEK)

- **Bağımlılık:** M2-01 · M1-05
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(sun): wrap and unwrap tag keys with the KEK`

**Amaç.** Etiket AES anahtarını DB'de sarmalı tutmak, yalnızca doğrulama
süresince bellekte açmak.

**Dokunulacak dosyalar.** `internal/sun/keys.go`, `keys_test.go`

**Kabul kriterleri.**
- `Wrap(key) → aes_key_ref` ve `Unwrap(ref) → key` (AES-GCM, `TAPPA_TAG_KEK`).
- Düz anahtar hiçbir log satırında, hata mesajında, template'te veya JSON
  çıktısında görünmüyor — testle kanıtlanır (hata mesajını yakala, içinde
  anahtar byte'ı arama).
- Yanlış KEK ile açma → hata, panik değil.
- Açılan anahtar kullanım sonrası sıfırlanıyor (`clear()`), en azından uzun
  ömürlü yapıya kopyalanmıyor.

**Tuzaklar.**
- Anahtarı struct alanında saklayıp cache'leme cazip (performans); yapma —
  bellek dökümü tüm parkı verir. Gerekirse ayrı bir ADR ile tartışılır.
- Test vektörlerindeki anahtarlar **sahte** ve dosyada öyle etiketli olmalı.

---

## M2-06 — Atomik sayaç ilerletme ve eşzamanlılık testi

- **Bağımlılık:** M1-08 · M2-04
- **Kırmızı çizgi:** §4.4 — **bu milestone'un en kritik görevi**
- **Commit:** `feat(sun): advance tag counter atomically`

**Amaç.** Replay'i tek atomik ifadeyle kapatmak ve bunu gerçek yarışla kanıtlamak.

**Sıra sabittir: CMAC doğrula → SONRA sayacı ilerlet.** Ters sırada saldırgan
geçersiz CMAC'li isteklerle sayacı ileri sürüp meşru tap'leri reddettirebilir (DoS).

**Kabul kriterleri.**
- Tek ifade: `UPDATE tags SET last_ctr = @ctr WHERE uid = @uid AND last_ctr < @ctr RETURNING uid`
- 0 satır (`pgx.ErrNoRows`) → replay/yarış → reject.
- **N goroutine aynı `(uid, ctr)` ile yarışır → tam 1 başarı**, `-race` ile.
  Bu test pazarlıksızdır; replay korumasının tek gerçek kanıtı odur.
- İleri atlama toleransı: `ctr > last_ctr` yeterli, `last_ctr + 1` beklenmez
  (biri telefonunu değdirip sayfayı açmamış olabilir).
- **Boşluk (`ctr − last_ctr − 1`) sonuçla birlikte döndürülür** — sabit bir eşik
  `internal/sun`'a gömülmez. Karar `base:ctr-gap-review` politikasınındır
  ([M3-06](m3-policy-motoru.md), Q21). Eski "> 1000 → flag" eşiği **kaldırıldı**:
  URL biriktirme ~10'luk boşluk üretir, 1000 onu hiç görmez.

**Tuzaklar — hepsi denetimde KRİTİK bulgudur.**
- `SELECT last_ctr` → Go'da karşılaştır → `UPDATE` (klasik TOCTOU).
- `sync.Mutex` ile korumak: tek process varsayımı; ikinci instance açılınca çöker.
- `>=` kullanmak: aynı `ctr` iki kez geçer.
- `RETURNING` sonucunu kontrol etmemek.

---

## M2-07 — `sun.Verify` ve test vektörleri

- **Bağımlılık:** M2-04 · M2-05 · M2-06
- **Commit:** `feat(sun): add Verify entrypoint with known-answer vectors`

**Amaç.** Parçaları tek giriş noktasında birleştirmek ve skill'deki vaka
tablosunun tamamını kanıtlamak.

**Dokunulacak dosyalar.** `internal/sun/verify.go`, `verify_test.go`,
`test/fixtures/sun_vectors.json`

**Zorunlu vakalar** (skill `tappa-sun`, hepsi bir test):

| Vaka | Beklenen |
|---|---|
| geçerli CMAC, `ctr = last_ctr + 1` | kabul, `last_ctr` güncellenir |
| geçerli CMAC, `ctr = last_ctr` | **reject** — replay |
| geçerli CMAC, `ctr < last_ctr` | **reject** — geriye sayaç |
| geçerli CMAC, `ctr = last_ctr + 5000` | kabul + boşluk değeri döner (karar politikanın) |
| yanlış anahtarla üretilmiş CMAC | **reject** |
| tek bit bozulmuş CMAC | **reject** |
| eksik/bozuk hex, yanlış uzunluk | **reject**, panik yok |
| bilinmeyen UID | **reject** |
| `retired` / `lost` etiket | **reject** |
| N goroutine aynı `(uid, ctr)` | tam **1** başarı (`-race`) |

**Kabul kriterleri.**
- `internal/sun` kapsamı **%90+** (`make cover`).
- `sun.Verify` karar vermiyor, `Result` döndürüyor (`sun_valid`, `tag`,
  `location`, `ctrGap`) — verdict M4'ün işi.
- Kullanıcıya dönen hata jenerik ("Couldn't verify that tap — try again");
  log'a ayrıntılı ama anahtarsız/CMAC'siz.
- `sun_vectors.json` sahte anahtarlarla, dosya başında öyle etiketli.
- Denetim: agent `tappa-security-auditor` R4 ve R7 temiz.

> **Kart düzeltmesi (2026-08-18, M2-08 uygulaması sırasında).** Bu kartın adı
> *"…ve test vektörleri"*, ama ürettiği vektörlerin **hepsi kendi zincirimizden**
> geldi: `verify_mac_test.go`'daki `referenceMAC` üretim `sv2()`+`truncateSDMMAC()`
> çağırıyor, `sun_vectors.json` de aynı şekilde üretildi. Böyle bir vektör kümesi
> **iç tutarlılıktan başka bir şey kanıtlayamaz** — kusur karşılaştırmanın iki
> tarafında birden bulunur.
>
> Kart, dış doğrulamayı *"gerçek çip M8-05'te"* diyerek ertelemişti. **O erteleme
> gereksizdi ve ölçümle gösterildi:** NXP AN12196 **yayımlanmış, tam ara değerli**
> worked example'lar içeriyor (§4.3 Tablo 2 s. 10; §4.4.4.2.1 Tablo 5 s. 15 —
> sonuncusu **boş MAC girdisiyle**, yani ADR 0003 madde 2'nin bizim tam
> yapılandırmamızla). Bunları transkribe etmek gerçek silikon **gerektirmiyor**;
> M2-08'de birkaç saatte yazıldı ve **ilk koşuşta kırmızı döndü** — çünkü
> M2-04'ün bayt sırası hatalıydı (yukarıdaki M2-04 düzeltmesi).
>
> **Ders, bu karta özgü değil:** "gerçek donanım gelene kadar dış vektör yok"
> cümlesi, donanım **belgesi** yayımlanmış ara değerler taşıdığında yanlıştır.
> Bir sonraki benzer erteleme yazılmadan önce belge taransın.
>
> Kartın "Zorunlu vakalar" tablosu geçerliliğini koruyor; eklenen şey
> `internal/sun/an12196_kat_test.go` ve **`referenceMAC`'i çağırmama** kuralıdır.

---

## M2-08 — SDM sayaç bayt sırası düzeltmesi + dış kaynaklı KAT

- **Bağımlılık:** M2-04 · M2-07 (ikisini de düzeltir)
- **Kırmızı çizgi:** §4.4 (replay/kanıt zinciri) · §4.7 (anahtar hijyeni)
- **Commit:** `fix(sun): feed SDMReadCtr into SV2 LSB-first and anchor the chain to AN12196`

**Amaç.** İki iş, ve ikincisi asıl olan:

1. `internal/sun/verify_mac.go` → `sv2()` sayaç baytlarını SV2'ye **ters**
   yazsın (URL MSB-first → SV2 LSB-first).
2. Zinciri **dış kaynaklı** known-answer vektörlerine bağla, ki bu sınıftan bir
   kusur bir daha sessizce yaşayamasın.

**Neden.** `sv2()` URL baytlarını **verbatim** ekliyordu ve bunu bir *yapısal
garanti* olarak yorumluyordu: *"çip SDMReadCtr'yi URL'ye SV2'ye verdiği sırayla
yazar — yani URL baytları SV2 baytlarıdır."* Bu cümle **yanlıştır**. NXP AN12196
rev. 1.8 aynı UID (`04C767F2066180`) için iki şeyi birlikte yayımlıyor:

| Yer | Ne diyor |
|---|---|
| §4.3 Tablo 2, **s. 10**, adım 4 | `SDMReadCtr = 010000` — *"(LSB first as per [Section 3.1])"* |
| §4.3 Tablo 2, **s. 10**, adım 7 | `SV2 = 3CC3 0001 0080 04C767F2066180 **010000**` |
| §4.4.1, **s. 11** | `…?uid=04C767F2066180&ctr=**000001**&c=…` |

Yani **URL metni MSB-first, SV2 girdisi LSB-first** — aynı değerin bayt-tersi iki
yazılışı. Bağımsız teyit: `icedevml/sdm-backend` → `validate_plain_sun` URL
baytlarını SV2 için ters çevirir (`read_ctr_ba.reverse()`), değeri ise `'>I'`
(big-endian) okur; şifreli PICC yolunda ise baytlar zaten LSB-first geldiği için
verbatim yazıp `'<I'` okur. İki yol birbiriyle ve belgeyle tutarlı.

**Bu kusur nasıl saklandı — asıl ders burada.** Üç şey üst üste geldi:

- **Vektörlerin hepsi kendi zincirimizden üretiliyordu.** `referenceMAC` üretim
  `sv2()`'yi çağırıyor; `sun_vectors.json` da aynı `sv2()` ile üretilmişti. Kusur
  karşılaştırmanın **iki tarafında birden** olduğu için fark edilemezdi. Dosya bunu
  kendi `_warning` bloğunda zaten **itiraf ediyordu** — uyarı doğruydu, okunmadı.
- **Yanlış davranışı çivileyen bir test vardı.** Adında *verbatim* geçen bir SV2
  testi, sayaç baytlarının URL baytlarına **eşit** olmasını şart koşuyordu.
  Yeşildi. Bir test kusuru da özellik kadar sıkı çiviler; ikisini ayıran tek şey
  **kodun dışından** bir kaynaktır. (Ölü adı burada **yazmıyoruz** — bu, ratchet'i
  `cmd/tappa/testnames_test.go` ile kurulan disiplinin ta kendisi; yerine geçen ad
  aşağıdaki kabul kriterinde.)
- **Kapsam yalan söylemedi, sadece başka şeyi ölçtü.** `internal/sun` kapsamı
  **%97** idi. Kapsam satırın *çalıştırıldığını* söyler, *doğru* olduğunu değil.

**Etkisi (ölçüldü).** 3 baytlık sayaçların palindrom olma oranı
65 536 / 16 777 216 = **1/256**, ve **1–255 aralığında hiç palindrom yok** →
gerçek bir çipin **ilk 255 tap'inin tamamı** reddedilirdi (`sun_valid=false` →
§5 satır 2). Kusur **sevk edilmiş ama tetiklenemezdi**, ve bunun kanıtı bir satır
sayısı değil bir **yokluk zinciri**: M8-05 encode runbook'u FAZ B'de duruyor,
encode aracı **hiç yazılmadı** (`cmd/` altında yok; `AuthenticateEV2First` /
`ChangeFileSettings` / `ChangeKey` uygulayan tek satır Go yok — repoda bu adlar
yalnız yorumlarda geçiyor) ve panelin plaket yaratma yolu da yok
(`db/queries/tags.sql` **hiç INSERT taşımıyor**). Encode edilmiş plaket yoksa
gerçek tap de yok.

🔴 **DÖRDÜNCÜ HALKA — ÜÇÜNÜ DOĞRULAYAN OKUR ONU BULUR, O YÜZDEN BURADA.** Üstteki
üç halka *"`tags`'a satır yazan bir yol yok"* demiyor: **var.**
`test/fixtures/seed.sql` doğrudan `INSERT INTO tags` yapıyor ve `scripts/seed.sh`
o satırların `aes_key_ref`'ini **gerçek `TAPPA_TAG_KEK`** ile sarmalıyor; yerelde
tablo altı haneli bir satır sayısı taşıyor. Bu yol yazılmazsa zincir eksik görünür.
Sonuç yine de ayakta, üç ölçülmüş sebeple: **(1)** sarmalanan düz anahtar
**sahtedir** — `seedkeys` onu public, bilerek gürültülü bir etiketten türetir
(*"self-evidently fake"*), gerçek bir çip anahtarı hiç ortada olmaz; **(2)**
`seed.sh` psql'i `docker compose exec -T db` ile çağırır, yani **yalnız yerel**;
**(3)** ve asıl sebep, **bir satır bir plaket değildir** — geçerli bir SUN, CMAC'i
hesaplayan **fiziksel bir çip** ister. Ayrıntı ve tam gerekçe:
[ADR 0003](../adr/0003-sdm-modu-ve-anahtar-yonetimi.md) → M2-08 ek notu.

**Dokunulan dosyalar.**
`internal/sun/verify_mac.go` · `internal/sun/params.go` (yorum) ·
`internal/sun/verify_mac_test.go` · **`internal/sun/an12196_kat_test.go` (yeni)** ·
`test/fixtures/sun_vectors.json` · `docs/adr/0003-sdm-modu-ve-anahtar-yonetimi.md` ·
`.claude/skills/tappa-sun/SKILL.md` (SV2 satırı sayaç bayt sırasını söylemiyordu;
listeye **sonradan** eklendi — ağaç onu değiştiriyordu, kart saymıyordu).

**Kabul kriterleri.**
- `sv2()` sayaç baytlarını **tam bir kez** ters çevirir; girdiyi (çağıranın
  `Params.CtrBytes` dilimini) **mutasyona uğratmaz**.
- Dış kaynaklı KAT dosyası **`referenceMAC`'i veya kod içi türetilmiş herhangi bir
  beklenti üretecini ÇAĞIRMAZ**; beklenen değerlerin hepsi AN12196'dan transkribe
  edilmiştir. Kapatılan her ara adım **ayrı** assert edilir (SV2 · oturum anahtarı ·
  boş girdi · kısaltma) ki kırmızı olduğunda **hangi adımın** bozulduğu okunsun.
- KAT'ın **ayırt ettiği** gösterilir: ters çevirmeyen SV2 belgenin oturum
  anahtarını üretmiyor (negatif kontrol), çift-ters çevrilmiş sayaç `verifyMAC`
  tarafından reddediliyor.
- Yanlış davranışı çivileyen SV2 testi **yeniden adlandırıldı ve yeniden
  yazıldı** → `TestSV2_CounterIsReversedIntoLSBFirstOrder`; yeni ad **gerçek
  özelliği** söylüyor. Eski adı anan **her yer** güncellendi (kod yorumu ve
  `sun_vectors.json` dahil) ve `cmd/tappa/testnames_test.go` ratchet sayısı
  **53'te bırakıldı** — ne büyütüldü ne küçültüldü.
- `sun_vectors.json` yeniden üretildi; `_warning` **daraltıldı ama kaldırılmadı**
  ve dosyaya **`_regenerate`** bloğu eklendi (bir dahaki sefere hangi komut).
- `internal/sun` kapsamı **%90+** kalır.

**Tuzaklar.**
- 🔴 **KAT dosyasının beklenen değerlerini "güncellemek" yasaktır.** Self-consistent
  bir golden kırmızı olunca değeri tazelenir; bu dosyada tam tersi geçerlidir —
  kırmızıysa **kod yanlıştır**. Bu kural dosyanın başında yazılıdır.
- **Palindrom sayaç seçmek testi değersizleştirir.** `000000`, `010101` gibi bir
  sayaç her iki bayt sırasında da geçer. Kullanılan vektörlerin sayaçları
  (`010000` / `3D0000`) bilinçli olarak **palindrom değildir**.
- **Ters çevirmeyi `params.go`'ya taşımak cazip; yapma.** `Params.CtrBytes`
  URL sırasını taşır ve `Params.Ctr` ile tutarlı kalmalıdır; ters çevirmenin
  **tek sahibi** `sv2()`'dir. İki yere koymak "iki kez ters çevir = verbatim"
  hatasını davet eder — negatif kontrol testi tam olarak bunu yakalar.
- **§4.4.1'in `c=54A45B2C3A558765` değeri KAPATILMADI.** O örneğin anahtarı
  belgede yayımlanmamış; altı hipotez denendi (Tablo 2 anahtarı × iki bayt sırası,
  sıfır anahtar × iki bayt sırası, iki farklı boş-olmayan MAC girdisi) ve hiçbiri
  üretmedi. Bu bir **sınırdır**, kusur değil; KAT dosyasında böyle yazılıdır.
  §4.4.1'den yalnızca sayacın **metni** kullanıldı (anahtar gerektirmez).
- **Gerçek silikon hâlâ görülmedi.** AN12196 bizim **decode** tarafımızı NXP'nin
  çip tarifine bağlar; **encode** tarafının (M8-05 runbook) sayacı URL'ye
  MSB-first mirror'ladığını kanıtlayamaz — bu ADR 0003 madde 1'in kararıdır ve
  vektörler onu **varsayar**.
- **Tablo 5, URL bayt sırasını İKİNCİ KEZ çivilemez.** Bu abartı bu görevin
  ikinci turunda yazıldı ve üçüncü turda düzeltildi. Tablo 5 **şifreli-PICCData**
  örneğidir: UID ve sayaç `PICCENCData`'nın **çözülmesinden** (adım 3) gelir ve o
  örnek için yayımlanan URL (§4.4.2.1, s. 11) `?e=…&c=…` biçimindedir — **düz
  `ctr=` hiç taşımaz**. Ölçüldü (`pdftotext -layout` çıktısı üzerinde): `00003D`
  dizisi **her iki revizyonda da 0 kez** geçiyor; KAT dosyasındaki `katT5URLCtr`
  bizim **türetmemizdir**. ⚠️ Aynı aramayı **boşlukları silerek** koşarsan rev 2.0'da
  bir sözde-isabet çıkar: sıfır anahtarın sonundaki `0000` + bir sonraki satırın
  adım numarası `3` + `D(`. Gerçek bir geçiş değil — metni **dizilmiş hâliyle** ara. URL sırası
  ekseninin **tek çapası** Tablo 2 + §4.4.1 aynı-UID eşleşmesidir (+ üçüncü parti
  teyit `icedevml/sdm-backend`). Tablo 5'in çivilediği şey ayrı ve gerçektir: SV2
  **düzeni**, türetme, **boş girdi**, kısaltma ve `sv2()`'nin girdisini gerçekten
  **ters çevirdiği** (`3D0000` palindrom değil).
- 🔴 **"Üretimde tetiklendi mi?" sorusunu `tags` SATIR SAYISIYLA ÖLÇME — RLS
  seni yanıltır.** M2-08'de ölçüldü, aynı an, aynı veritabanı:

  | Rol | `select count(*) from tags` |
  |---|---|
  | `tappa_app` (`DATABASE_URL`) | **0** — RLS bağlamı yokken hep 0 |
  | `tappa_owner` (`DATABASE_MIGRATE_URL`) | **altı haneli** — kendin ölç, aşağıdaki komut |

  ⚠️ **Sağdaki hücrede bir zamanlar `103 267` yazıyordu; o sayı ölçüldüğü ANA
  aitti ve bugün yanlıştır.** Ne kadar canlı olduğu ölçüldü: bu satırın yazıldığı
  turda aynı komut önce **105 040**, tek bir `make check` sonrasında **105 520**
  verdi — yani sayı bir doğrulama turu içinde bile oynuyor (testler ve `make seed`
  yazıyor). Kararı taşıyan şey **sağ hücrenin değeri değil, iki hücrenin FARKI**;
  o yüzden sayı burada dondurulmuyor, yerine komutu yazılıyor:

  ```bash
  docker compose exec -T db psql -U tappa_owner -d tappa -tAc "select count(*) from tags;"
  ```

  `app.tenant_id` kurulmamış bir bağlantı hiçbir satır göremez (CLAUDE.md §6),
  yani **boş tablo ile dolu tablo uygulama rolünden aynı görünür** — hipotezin
  doğru da olsa yanlış da olsa aynı sayıyı alırsın. Bu soru **yokluk zinciriyle**
  yanıtlanır (encode aracı yok → plaket yok → tap yok), sayımla değil.

**M8-05 ile ilişkisi.** Bu görev M8-05 FAZ B'yi **bloklamıyor** (düzeltme ve
vektörler hazır). Tersine yön geçerli: M8-05'te ilk gerçek plaket encode edilip
tap edildiğinde, o tap **bu düzeltmenin encode tarafındaki karşılığını** ilk kez
kanıtlar. FAZ B'nin runbook'u sayacı URL'ye **MSB-first** yazmak zorundadır;
aksi hâlde `sv2()` doğru olduğu hâlde doğrulama başarısız olur.
