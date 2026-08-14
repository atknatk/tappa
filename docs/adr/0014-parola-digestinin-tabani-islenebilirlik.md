# ADR 0014 — `admin_users.password_hash` tabanı: "işlenebilirlik", güç değil

- **Durum:** kabul edildi
- **Tarih:** 2026-08-13
- **Karar veren:** M7-03 Faz A yapıcısı. Sıkılık kararı orkestratör brief'inin
  *"her ikisinin bedelini ölç, birini seç, gerekçesini sayıyla yaz"* yetkisiyle
  verildi.
- **Etkilenen:**
  [`db/migrations/00018_admin_password_hash_must_be_bcrypt.sql`](../../db/migrations/00018_admin_password_hash_must_be_bcrypt.sql)
  · [`internal/db/adminpasswordhash_test.go`](../../internal/db/adminpasswordhash_test.go)
  · [`test/fixtures/ids.go`](../../test/fixtures/ids.go)
  · altı test dosyasının admin fixture'ı (sekiz nokta)
  · [`internal/domain/signup/signup_db_test.go`](../../internal/domain/signup/signup_db_test.go)
  · [`internal/adminauth/password.go`](../../internal/adminauth/password.go)
  (davranışı değişmedi; **süresi dolan gerekçesi** kapandı)
- **Migration VAR:** 00018. Yeni tablo yok, **GRANT'a dokunulmadı** — 00017'nin
  sütun bazlı INSERT/UPDATE yetkileri aynen duruyor; bu CHECK o yetkilerin *kimde*
  olduğunu değil *ne taşıyabildiğini* daraltıyor.

---

## Bağlam — süresi dolan gerekçe doldu

`internal/adminauth/password.go` dört zamanlama kolunu ölçmüş ve *"bugün delik
değil, ama gerekçe süresi dolacak"* diye yazmıştı:

```
unknown email (cost-12 dummy)     297,9 ms          1x
geçerli cost-12 digest            294,6 ms          1x
cost-4 digest                       1,42 ms        210x
password_hash = ''                   198 ns    1 504 663x
bozuk digest                         154 ns    1 934 567x
```

Gerekçe *"repoda `INSERT INTO admin_users` yok"*tu ve süresini de adıyla yazmıştı:
**M7-02**. M7-02 sevk edildi (`internal/store/admins.sql.go:161`), yol açıldı.

**Ölçüldü** (migration ÖNCESİ, `tappa_app` rolüyle, kendi tenant bağlamında,
`BEGIN … ROLLBACK` içinde):

```
INSERT … password_hash = ''                  -> INSERT 0 1
INSERT … password_hash = 'not-a-real-hash'   -> INSERT 0 1
UPDATE admin_users SET password_hash = ''    -> UPDATE 158
```

Son satır kararı verdi: **tek ifade**, uygulama rolü için yasal, bir tenant'ın
**bütün** yöneticilerini nanosaniye hızında bir sayım kehanetine çeviriyor.

---

## Karar 1 — "yalnız biçim" diye bir seçenek YOK

Doğal ilk taslak `^\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}$` idi: önek, iki hane
maliyet, 53 karakter gövde, toplam 60. **Ölçüldü** (x/crypto, 00–99 arası her
maliyet için bir karşılaştırma):

| maliyet | davranış |
|---|---|
| **04–31** | anahtar programını **öder** (cost 4'te 1 ms … cost 12'de 300 ms) |
| 00–03 | **0–2 µs**'de hata — `cost 3 is outside allowed range (4,31)` |
| 32–99 | **2 µs**'de hata — aynı sebep |

Yani `[0-9]{2}`, hızlı yola giden **100 iki-haneli maliyetin 72'sini** kabul
ediyor. O taslak `''` ve `'not-a-real-hash'`'i reddedip `$2a$99$cccc…`'yi kabul
ederdi — 60 karakter, doğru şekil, **2 µs**'de ret: aynı 1,9 milyon kat kol,
başka bir yazımla.

**Bu yüzden kısıtın kodladığı değişmez bir yazım değil, bir özellik:**

> Bu sütunun **bundan sonra kabul edebildiği** her değer, bcrypt'e anahtar
> programının **tamamını** ödetir.

⚠️ *"kabul edebildiği"*, *"tutabildiği"* değil — arada **20 232 satır** var. Kısıt
`NOT VALID`, yani dev DB'de zaten duran hızlı-cevaplayan digest'ler duruyor.
Ürün verisi temiz (iki seed satırı `$2a$12$`), yeni ihlal **yazılamıyor**.

Bu özelliğin bir testi var (`TestPasswordHashCheck_EverythingItAdmitsIsProcessableByBcrypt`):
regex'i Go'da yeniden yazıp kendisiyle karşılaştırmıyor — **iki otoriteye ayrı ayrı**
soruyor (Postgres "kabul eder misin", x/crypto "ayrıştırabilir misin") ve veritabanı
kütüphanenin ayrıştıramadığı bir şeyi kabul ederse kırmızıya dönüyor.

## Karar 1b — maliyet **tavanı 14**: bu bir DoS sınırı, biçim sınırı değil

İlk sürüm bcrypt'in kendi maksimumunda (31) bitiyordu. **Bu altta doğru, üstte
yanlış** — iki uç farklı bozuluyor: 4'ün altı karşılaştırmayı fazla **hızlı**
yapar (yukarıdaki kehanet), 31'e yakını sınırsız **yavaş**. Ölçüldü:

**Referans ölçüm** — darwin/amd64, go1.26.5, **`-race` YOK**, **min-of-3**,
**yük ortalaması 5,54 → 5,79**, 2026-08-13. Bu ADR'deki, `00018`'deki ve
`password.go`'daki her rakam **bu tek koşudan**:

| maliyet | tek karşılaştırma | 8 aday |
|---|---|---|
| 12 (sevk edilen) | 214 ms | 1,7 s |
| 13 | 423 ms | 3,4 s |
| **14 (tavan)** | **892 ms** | **7,1 s** |
| 15 | 1,78 s | 14,2 s |
| 31 (türetilmiş) | **~31 SAAT** | **~249 SAAT** |

⚠️ **Ve bant, çünkü tek sayı yazmak bu repoda üç kez yanlış çıktı** (Makefile:
*"HER SAYI BIR YUK KOSULUYLA BIRLIKTE YAZILIR"*). `-race`'siz cost 12 için bu
oturumdaki ve bağımsız denetimdeki dört okuma: **210 / 214 / 307 / 312 ms** — 1,5×
fark, tek sebebi makine yükü (denetimin 312 ms'i yük 7,36'da). **Yavaş uç taban
alınırsa** cost 14 ~1,25 s ve 8 adayda ~10 s, cost 15 ~2,5 s / ~20 s, cost 31
~45 saat. **Karar bandın her yerinde aynı** — yavaş uçta *güçleniyor*.

🔴 **Ve patlama yarıçapı satırın sahibi değil.** `manager.go:475` her girişi
`MaxCandidates` karşılaştırmaya dolduruyor ve dolgunun maliyetini **ilk adayın
digest'inden** alıyor; üstündeki döngü **her** adayı karşılaştırıyor. Adaylar bir
e-postayı paylaşan tüm admin satırları (00017'nin çözücüsü). Yani **tek** yavaş
satır, o adresin **bütün** giriş denemelerini — başka bir tenant'taki meşru
sahibinin denemesi dahil — durduruyor.

**Neden 14:** ürünün hâlâ **çalıştığı** en yüksek maliyet (8 adayda ~7 s; 15 bunu
kullanılamaz hale getiriyor). `adminauth.Cost = 12`'nin üstünde **iki katlama**
başlık payı bırakıyor. Bugünkü bedeli **sıfır**: repo yalnız iki maliyet üretiyor
(4 ve 12), dev DB'de yalnız 04/10/12 var — sayıldı. **Elenen şık** (tavan koymamak)
0 kazandırıyor ve **132.000×** yarıçap farkı kaybettiriyor.

⚠️ Bundan sonra `Cost`'u 14'ün üstüne çıkarmak **migration gerektiriyor** — kasıtlı:
o değişiklik her girişi 8× pahalılaştırır ve gözden geçirilmiş bir şema olayı olmalı.

## Karar 2 — maliyet **tabanı** 04, ve 10 elenirken sayısı yazıldı

Masadaki daha sıkı seçenek `cost ≥ 10` idi; 210× kolunu da kapatırdı. **Alınmadı.**

`make test` `-race` ile koşar ve yarış dedektörü blowfish'in anahtar programındaki
her bellek erişimini enstrümante eder. **Bu makinede ölçüldü**, tek karşılaştırma:

| | `-race` YOK (yük 5,54) | `-race` VAR (yük 4,77) |
|---|---|---|
| cost 4 | 1 ms | **15 ms** |
| cost 10 | 52 ms | **722 ms** (cost 4'ün **48×**'i) |
| cost 12 | 214 ms | **2 837 ms** (cost 4'ün **187×**'i) |

⚠️ Oran da yüke bağlı: aynı gün yük 7,7'de tek atışlık okumalar cost 10 için
**1 068 ms (67×)**, cost 12 için **4 962 ms (310×)** verdi. Yani cost 10 suite'e
cost 4'ün **48–67×**'ini ödetiyor. **Şıkkı eleyen sayı bir oran değil**, aşağıdaki
uçtan uca duvar saati.

Cost 10, `≥10` kısıtının kabul edeceği **en ucuz** digest'tir; yani bu bant bir
tahmin değil, o kısıtın **taban** bedelidir. Doğrudan koşuldu — `internal/adminauth`
fixture'ları cost 4'ten cost 10'a alındı:

```
fixture cost 4  ->  138,647 s
fixture cost 10 ->  295,054 s      (+156,4 s · 2,13×)
```

Ve bu **tek paket**. `internal/handler` de cost-4 literalleri kullanıyor ve zaten
suite'in kabul edilmiş ~256 s'lik tavanı. İki test dosyası bu faturayı ödeyip geri
aldığını **zaten kaydediyor**: `manager_db_test.go` (*"internal/adminauth Go'nun
10 dakikalık paket zaman aşımına 609 s'de takıldı — gerçek bir arıza"*) ve
`adminlogin_db_test.go` (*"bu paket ~140 s'den 571 s'ye çıktı"*). `≥10` kısıtı o
geri almayı **veritabanı düzeyinde yasadışı** kılardı.

### 04'te durmanın açık bıraktığı — düz yazıyla

Bir cost-4 satırı hâlâ dummy'den **210× hızlı** cevap verir. O kol **başka yerde**
kapalı ve iki yer denetlenebilir olsun diye adıyla yazılıyor:

- `adminauth.Cost = 12` — bu reponun `Hash`'inin ürettiği tek maliyet; tek üretim
  yazarı `internal/domain/signup/signup.go:621`.
- `internal/domain/signup/signup_db_test.go` **saklanan satırın** `$2a$12$` ile
  başladığını doğruluyor — digest'i **veritabanından geri okuyor**, yani sihirbaz
  daha ucuz bir şey yazacak şekilde yeniden bağlanırsa kırmızıya döner.

Yani: **işlenebilirlik tabanı yapısal, maliyet tabanı disiplinsel.** Bu "sütun
güvende"den daha küçük bir iddia ve doğru olan bu.

---

## Sonuçlar

- **İyi:** boş dize, bozuk digest, aralık dışı maliyet **ve 14 üstü maliyet** artık
  **yazılamıyor** — ne INSERT ile ne UPDATE ile, `tappa_app` dahil hiçbir rolle.
  1,5M× ve 1,9M× hızlı kolları ve ~249 saatlik yavaş kol kapandı.
- **Bedel:** `NOT VALID`. Geliştirme veritabanındaki **20 232 / 37 873** ihlal eden
  satır (2026-08-13; sayı her koşuda büyür) yerinde kalıyor ve **donuyor** — o
  satırların herhangi bir sütununa UPDATE reddediliyor. Ürün verisi **temiz**.
  00013'ün `tags` için kaydettiği takasın aynısı.
  ✅ **Ama donma geri alınabilir** (ölçüldü): donmuş satıra *yasal bir digest yazan*
  UPDATE **başarılı** — çaresi şema müdahalesi değil, sıradan bir **parola
  sıfırlaması**.
- ⚠️ **`VALIDATE`'i hiçbir şey koşturmuyor.** Boş tabloda bile `convalidated = f`
  doğuyor ve `pg_dump` bunu taşıyor. *"Doğuştan valide"* **zorlama** için doğru,
  **katalog** için değil. Koşacak yer: `make db-reset` sonrası, ve üretimde
  cutover'dan sonra (boş tabloda anlık no-op).
- **Bedel:** **6 dosyada 8 fixture noktası** — sayı elle yazılmadı, diff'ten
  türetiliyor (koşuldu, `8` verir):
  `git diff -U0 -- '*_test.go' | grep '^-' | grep -cE "VALUES.*('x'|not-a-real-hash|2a.12.aaa)"`.
  `'x'` / `'not-a-real-hash'` / 29 karakterlik kırık digest yerine
  `fixtures.UnusablePasswordHash` yazıyor.
  Anlamları değişmedi ve bu **mutasyonla** doğrulandı — ⚠️ *beklendiği yerden
  değil*: Postgres **RLS WITH CHECK'i tablo CHECK'inden ÖNCE** değerlendiriyor
  (doğrudan ölçüldü: çapraz tenant `tenant_id` + `password_hash = 'x'` →
  yalnızca *"new row violates row-level security policy"*). Yani `rls_test.go`'nin
  **forge** kolunu `'x'`'e geri çevirmek testi **yeşil bırakıyor**; fixture'ı
  zorlayan şey **pozitif kontrol** — B'nin *kendi* satırını yazdığı kol 23514 ile
  patlıyor. Bir güvenlik kısıtının izolasyon testini bozup bozmadığını "hangi kol
  önce konuşur"a bakmadan varsaymak burada yanlış cevap verirdi.
- **Riziko:** ileride `password_hash`'e yazan bir yol (M6-05, M7-04) `adminauth.Hash`
  yerine kendi digest'ini üretirse, maliyet tabanı disiplinsel olduğu için 210×
  kolu geri gelir. Bunu yakalayan tek şey signup'ınkine benzer bir "saklanan satır
  `$2a$12$` ile başlıyor" testidir; o yolları açan görevler bunu miras almalı.
- **Geri alma bir güvenlik gerilemesidir** ve migration'ın `Down` bloğu bunu
  yazıyor: kısıt düşerse `UPDATE … SET password_hash = ''` yeniden yasal olur.
