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
