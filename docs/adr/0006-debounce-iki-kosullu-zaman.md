# ADR 0006 — Debounce sunucu tarafı bir gerçeğe dayanır

- **Durum:** kabul edildi (ilk uygulama **yetersizdi**, aynı gün genişletildi — bkz. "Kusur üç katmanlıydı")
- **Tarih:** 2026-08-01
- **Karar veren:** kullanıcı (2026-08-01), [M5-08](../plan/m5-tap-akisi.md#m5-08--qr-kanalı)
  denetim turunda ölçülen kusur üzerine
- **Etkilenen:** `internal/domain/tap` (karar motoru) · `internal/domain/checkin`
  (eşleme) · CLAUDE.md §5 satır 5'in ifadesi

## Bağlam

[CLAUDE.md](../../CLAUDE.md) §5 satır 5 şunu söyler: *"Aynı **kişi** 60 sn içinde
tekrar → `ignored`."* Bu kural §5'in bir başka cümlesiyle birlikte anlam kazanır:
*"Bir sonraki işlem zaten yeni fiziksel dokunuş gerektirir."*

NFC'de bu cümle **yapısaldır**: `tags.last_ctr`'ın tek ifadeli atomik
ilerletmesi ([ADR 0003](0003-sdm-modu-ve-anahtar-yonetimi.md), §4.4) çipin ürettiği
sayaç değerini harcar, yani tek bir dokunuşun URL'i ikinci kez POST edilemez —
ikincisi kaydedilmiş bir replay reddidir.

**QR'da öyle değildir**, çünkü harcanacak sayaç yoktur (`ctr`/`cmac` yok,
`sun_valid=false`). M5-05 devir notu bunu doğru öngörmüştü: *"QR'da ilerletilecek
sayaç yok → tek savunma 60 sn person-scoped debounce."*

O tek savunmanın **çalışmadığı M5-08'de ölçüldü.**

## Ölçülen kusur — DÖRT KATMAN

Debounce girdisi `internal/domain/tap/decide.go` içinde şöyle hesaplanıyordu:

```go
gap := in.Now.Sub(in.LastForPerson.OccurredAt).Seconds()
```

`in.Now` **sunucu** saatidir. `LastForPerson` ise
`GetLastTransactionForEmployee`'nin döndürdüğü satırdır ve o sorgu
**`ORDER BY occurred_at DESC`** yapar. Yani hem **değer** hem **satır seçimi**
istemcinin beyan ettiği bir sütuna dayanıyordu. Kusur bu yüzden tek değil **dört
katmanlıydı**, ve **her düzeltme turu bir sonrakini ortaya çıkardı** — bu ADR'nin
en öğretici kısmı budur. Dördüncüsü `occurred_at` ile hiç ilgili değil:
**okuma-sonra-karar**, yani CLAUDE.md §4.4'ün sayaç için adıyla yasakladığı şekil.

Üçü de gerçek Postgres'e karşı, monte edilmiş router üzerinden, tek taranmış bir
QR bağlamıyla ölçüldü:

| Katman | Mekanizma | Ölçüm |
|---|---|---|
| **1. MESAFE** | geçmiş bir `occurred_at` beyan et → gap devasa görünür | 20 POST → **20 sayılan satır**, 0,51 sn |
| **2. SEÇİM** | kişinin **mevcut en yeni satırının altına** bir zaman beyan et → yazılan her satır onun altına sıralanır, `LastForPerson` **hiç ilerlemez**, iki bacak da aynı dokunulmamış eski satırı ölçer | 30 dk önce sıradan bir tap'i olan çalışan, `occurred_at = now−71sa` (72 sa sınırının içinde, kişinin geçmişini **bilmeyi gerektirmez**): 20 POST → **20 sayılan satır**, 0,31 sn |
| **4. EŞZAMANLILIK** | `gather` ve `write` **ayrı transaction**'lardı → aynı anda gelen N istek hiçbiri henüz yazmamışken okur, hepsi aynı öncülü görür, hiçbiri tekrar sayılmaz | 50 eşzamanlı POST → **51 sayılan**, 0 `ignored`, 0,41 sn · **2 eşzamanlı POST bile yetiyor** |
| **3. İŞARET** | `sys:occurred-at-bound` ileri tarihi **reddeder** ama §4.6 satırı yine **yazar**; o satır `ORDER BY occurred_at`'i kazanır ve `now − occurred_at` **negatif** olur. Guardrail `gap >= 0 && gap < window` ile eşleştiği için **hiç ateşlenmez** | 1 ileri tarihli POST (`now+48sa`), sonra **hiçbir zaman beyan etmeyen** 20 dürüst tap → **20 `ok`**, 0,29 sn |

**3. katmanın ağırlığı ayrıca not edilmeli:** sonuç `flag` değil **`ok`**. Yani
müdür onay kuyruğunda hiçbir şey görmez; kayıtlar sessizce saate girer.

**4. katman `occurred_at`'ten bağımsızdır** ve hiçbir beyan gerektirmez: istekler
**hiçbir zaman beyan etmeden**, dürüst canlı tap şeklinde gönderilir. Aynı şekil
**practice**'i de çoğaltıyordu — hiç tap'i olmayan bir çalışanda 20 eşzamanlı
POST → **20 satırın 20'si de `practice=true`** (saate girmediği için zararsız,
ama aynı okuma-sonra-karar kusuru).

**Kontrol — NFC ilk üç katmana bağışık** (4. katman kanaldan bağımsızdır, çünkü
sorun kararın kendisindedir): aynı döngüler NFC kanalında
**1 satır + 19 `reject`/`sys:sun-invalid`**, `last_ctr` **700 → 701**. Koruyan şey
debounce değil **atomik sayaç ilerletmesi**. Kusur, sayacı olmayan kanalda
görünür hâle gelen bir **girdi kusuruydu**.

**Yayılma yarıçapı ölçüldü (ve dardır).** İleri tarihli satır **yalnız**
debounce'u bozuyor: yön zinciri etkilenmiyor, çünkü ileri tarihli her satır
tanımı gereği `sys:occurred-at-bound` ile **reddedilmiştir**, reddedilen satır
`type` taşımaz ve `GetLastOpenTransaction` `type='in'` filtresiyle onu hiç
görmez. Ölçüldü: zehirden sonraki dürüst tap hâlâ doğru şekilde `out`. Geç kalma
hesabı da etkilenmiyor (o, **bu** tap'in `occurred_at`'i ile vardiyadan hesaplanır,
önceki satırdan değil).

**Gerçek tavan da ölçüldü, ve o debounce değildi.** Bağlayıcı sınır oturum oran
bütçesidir ve **bir hız değil bir patlamadır** (`httpx.Limiter` sabit pencere):
**300 istek arka arkaya ~1,2 sn'de servis edildi, 301.'si 429**. 15 dk'lık
imzalı-bağlam TTL'ine iki 10 dk'lık pencere sığdığı için yapısal tavan **~600
istek** ≈ **~599 yön taşıyan satır**.

⚠️ **§4.6 ihlali değil:** her satır yazılıyor ve atfedilebilir. Ama 3. katmanda
satırlar `flag` bile değil `ok` olduğu için **insan denetimine de düşmüyorlardı**.

⚠️ **`occurred_at` sınırsız değil:** `sys:occurred-at-bound` kapatılamaz ve
tolerans dışını reddeder (ölçüldü: 71sa50dk → **200**,
`flag`/`base:queued-window`; 72sa10dk → **`reject`/`sys:occurred-at-bound`**).
Yani kapı kapalı değil, **72 saat genişliğinde** — 2. katman için fazlasıyla
yeterli.

## Karar

Debounce girdisi **iki mesafenin küçüğüdür**, ve ikinci bacak **ayrı, sunucu
tarafından sıralanmış bir gerçekten** okunur:

```
gap = min( now − LastForPerson.OccurredAt ,  SecondsSinceLastRecordedTap )
```

⚠️ **İkinci terim bir ÇIKARMA DEĞİL, hazır bir yaştır** — ve bu, ilk yazımın
yanlış olduğu yerdi (aşağıdaki 🔴 paragraf). Go hiçbir DB timestamp'ini kendi
saatinden çıkarmaz.

- **Beyan bacağı** — `LastForPerson.OccurredAt`. İstemci hem değerini hem hangi
  satır olduğunu etkileyebilir, ama `min` yüzünden gap'i yalnız **küçültebilir**,
  asla büyütemez.
- **Sunucu bacağı** — yeni sorgu **`SecondsSinceLastRecordedTap`**:
  ```sql
  SELECT EXTRACT(EPOCH FROM (clock_timestamp() - created_at))::float8
  FROM transactions
  WHERE tenant_id = @tenant_id AND employee_id = @employee_id
    AND channel IN ('nfc','qr')
    AND created_at > clock_timestamp() - make_interval(secs => @window_seconds)
  ORDER BY created_at DESC LIMIT 1;
  ```
  Son satır **debounce penceresi sınırıdır**; hiçbir kararı değiştirmez (pencerenin
  üstündeki bir yaş `min`'i zaten kazanamaz), sebebi aşağıdaki **"Sınır 3"**.
  **`created_at`'e göre sıralanır** → ne değeri ne seçimi istemciden erişilebilir
  (**2. katmanı kapatan budur**; ilk uygulamada bu bacak `LastForPerson`'ın bir
  sütunundan okunuyordu, yani yanlış satırdan). **Ve yaşı SQL hesaplar** — Go'ya
  bir timestamp döndürüp çıkarmayı ona bırakmak 4. katmanda ölçümle çöktü.

**İki uygulama kararı, ikisi de ölçümle:**

- **Negatif beyan bacağı DÜŞÜRÜLÜR, sıfıra ÇEKİLMEZ** (3. katman). Düşürmek
  **saldırgana karşı fail-closed**tır: hiçbir şey kazanmaz, çünkü sunucu bacağı
  cevap verir. Sıfıra çekmek ise **kullanıcıya karşı** fail-closed olurdu — tek
  bir ileri tarihli satır, o tarih gelene kadar (72 saate kadar) o kişinin
  **her gerçek tap'ini** `ignored` yapardı; kendi hatası bir kez, bedeli günlerce
  saat kaybı. Gelecek hakkında bir iddia yakınlığın kanıtı değildir; kanıt
  değildir, o yüzden sayılmaz. *(Sıfıra çekme mutasyonu testte **RED**.)*
- **Saat kayması da düşürülür.** `created_at` veritabanının, `Now` uygulamanın
  saatidir; veritabanı ilerideyse çıkarma negatife düşer ve alınırsa meşru bir
  tap yutulur.

**Manuel istisnası SORGUNUN İÇİNDEDİR** (`channel IN ('nfc','qr')`), Go'da değil.
`created_at` bir manuel satırda **müdürün yazdığı andır** ve çalışanın nerede
olduğuna dair hiçbir iddia taşımaz. İstisna olmadan iki meşru akış kırılır:
(1) toplu manuel giriş — iki satır `created_at`'te saniyeler arayla; (2) daha
kötüsü, **çalışanın gerçek tap'i** müdürün geçmişe tarihli girişinden saniyeler
sonra yutulur. Sorguda ifade etmek onu **erişilemez** kılar: `channel` sunucu
türetimlidir ve `manual` ayrıca `entered_by` (müdür kimliği) ister — tap eden bir
istemci kendisi için manuel öncül **üretemez**. *(Yüklem silindiğinde test
**RED**.)*

**Verdict'e göre FİLTRELEME YOK — bilinçli.** Sunucu bacağı `reject` satırlarını
da sayar. İki sebep: (a) reddedilmiş bir tap **yine de fiziksel bir dokunuştur**,
"son 60 sn içinde bu kişi dokundu mu" sorusunun cevabı evettir; (b) verdict'e
göre filtrelemek çağırana bacağı **boş tutmanın bir yolunu** verirdi (reddedilecek
tap'ler üreterek). Ölçülen sonucu: 3. katman sondasında zehirden sonraki 20
dürüst tap **20 `ignored`** olur — 0 sayılan. *(`state.md` M5-05 devri md.5
"debounce temeli herhangi bir verdict" durumunu zaten biliniyor risk olarak
taşıyor; bu ADR onu **bilinçli tercih** hâline getiriyor.)*

**4. KATMAN: KİŞİ BAŞINA ADVISORY LOCK (kullanıcı kararı, 2026-08-01).**
`gather` + `Decide` + `write` artık **tek bir transaction**tır ve o
transaction'ın **ilk ifadesi** `pg_advisory_xact_lock`tır. Anahtar
`hashtextextended(tenant_id || ':' || employee_id)` — 64 bit, ve **tenant
anahtarın içinde olmak zorunda**, çünkü advisory lock'lar küme genelinde tek bir
uzayda yaşar ve RLS ile kapsanmaz. Hash çakışması iki ilgisiz kişiyi serileştirir.
Veriler **karışamaz** (içerideki her ifade kendi `tenant_id` filtresini ve RLS'i
taşır), ama **bekleme küçük değildir ve "başka hiçbir etki" de yoktur** — bu ADR
bir tur boyunca *"birine birkaç milisaniye, başka hiçbir etki"* yazdı ve **ikisi
de** yanlıştı; aşağıdaki havuz-parkı bölümü zaten 15/16 bağlantı ve **6–9×**
diyor. Kaybeden taraf **kilitli bölümün kalanının tamamını** bekler: ölçüldü,
anahtarı 2 sn tutan bir oturumun 0,4 sn ardından gelen çakışan oturum
**1778,890 ms** bekledi. Tavan `middleware.Timeout(30s)`.

- **Kilit transaction kapsamlıdır**, yani COMMIT/ROLLBACK onu bırakır; hiçbir hata
  yolu sızdıramaz.
- **Kilidin dışında bırakılanlar, bilerek:** (a) **politika seti** yukarıda
  çözülür — bir tenant'ın baseline'ını materyalize etmek kendi transaction'ını
  açar ve bunu kilit tutarken yapmak iki havuz bağlantısını iç içe sokardı;
  (b) **atomik sayaç ilerletme** adım 2'de, **tag tenant'ının kendi bağlamında**
  kaldı — §4.4'ün ve M5-05 denetiminin koyduğu yerde. Sıra bozulmadı.
- **Kilit tutulurken ağ işi yok:** `tap.Decide` saftır; içerideki tek G/Ç bu
  tenant'ın kendi okumaları ve tek bir INSERT.

🔴 **VE İLK DENEME BURADA DA YETMEDİ — ölçüm gösterdi.** Kilit doğru çalışıyordu
(iki eşzamanlı transaction ölçüldü: ikincisi 365 ms bekledi) ama serileşen
istekler **yine `ok`** dönüyordu. Sebep bu ADR'nin kendi 2. bacağıydı: sorgu
`created_at`'i **timestamp olarak** döndürüyor ve Go onu **kendi saatinden**
çıkarıyordu. Uygulama `now`'ı **kilidi beklemeden ÖNCE** yakalar; `created_at` ise
**daha sonraki** bir transaction'ın başlangıç zamanıdır → çıkarma **negatife**
düşer ve "saat kayması" koruması bacağı **atar**. Ölçülen: `now=…46.400` iken
öncülün `created_at`'i **…46.475**, yani 75 ms **gelecekte**.
**Çözüm:** sunucu bacağı artık bir **yaş** döndürüyor ve yaşı **veritabanı**
hesaplıyor —
`EXTRACT(EPOCH FROM (clock_timestamp() − created_at))`. İki uç da **aynı saat
domaininde**, yani Go/DB ayrışması bu bacağı artık sessizce düşüremiyor.
⚠️ **Ama "kayma diye bir şey kalmadı" DEĞİL, ve fark ölçüldü:** yazma ucu hâlâ
`created_at DEFAULT now()`, yani **transaction başlangıcı**. Kilidi W saniye
bekleyen bir isteğin satırı W saniye geçmiş bir `created_at` taşır (doğrudan
ölçüm: 3 sn bekleyen tx'te `clock_timestamp() − now() = 3,007 sn`), dolayısıyla
**bir sonraki isteğin ölçtüğü yaş W kadar ŞİŞER**. Bugün sömürülebilir değil —
W en fazla `middleware.Timeout` (30 sn) ve 60 sn'lik pencerenin altında, üstelik W
yalnız kişinin **kendi** kuyruğuyla oluşur — ama sıfır da değil. Yazma ucunu da
`clock_timestamp()`'a çekmek migration ister; **ölçüldü, yapılmadı, yazıldı**. `clock_timestamp()`, `now()` **değil**:
`now()` transaction başlangıcıdır ve kilidi bekleyen bir istekte ölçtüğü satırdan
**öncedir**.
*(Bu, denetimin N1 notunu da kapatıyor: Go saati ile DB saatinin ayrı hostlarda
ayrışması artık bu bacağı sessizce düşüremez, çünkü Go saati bu bacağa hiç
girmiyor.)*

**Ölçülen sonuç (aynı sondalar, kural + kilit sonrası):**

| Sonda | Önce | Sonra |
|---|---|---|
| 50 eşzamanlı POST (geçmişli çalışan) | 51 sayılan, 0 `ignored` | **50 satır, 1 yeni sayılan, 49 `ignored`** |
| 2 eşzamanlı POST | 2 sayılan | **1 sayılan + 1 `ignored`** |
| 20 eşzamanlı, hiç tap'i olmayan çalışan | **20 `practice`** | **1 `practice`** |
| 30 FARKLI kişi eşzamanlı | 0 non-200, 30 sayılan, 0,42 sn | **0 non-200, 30 sayılan, 0,37 sn** |

**§4.6 korunuyor:** 50 istek → **50 satır**. Hiçbiri reddedilmiyor; fazlalar
`ignored` olarak yazılıyor.

**Havuz ölçüldü, varsayılmadı:** 30 farklı kişi aynı anda → **hiç non-200 yok**,
hepsi sayıldı, ve süre kilit **öncesiyle aynı** (0,37 sn vs 0,42 sn). Aynı kişi
için 50 eşzamanlı istek sıraya giriyor ve tamamlanıyor (0,41 → 0,87 sn, istek
başına ~17 ms).

🔴 **AMA KİLİDİN İLGİSİZ KİŞİLERE BİR BEDELİ VAR — ve bu ADR bir tur boyunca
tersini yazdı.** *"Kilide atfedilebilir yeni bir DoS yüzeyi yok"* cümlesi
**ölçümle yanlışlandı** (kırmızı çizgi denetimi). Sebep: **bekleyen istek havuz
bağlantısını tutar**. Tek bir anahtara inen flood, hiçbir iş yapmayan
bağlantıları park ediyor — `pg_stat_activity` örneklemesi tek anahtar kolunda
`wait_event='advisory'` olan **16 bağlantının 15'ini** gösterdi; aynı satır
hacmi ayrı anahtarlara dağıtıldığında **0**. Başka tenant'ların istekleri
`pool.Begin`'de kuyruğa giriyor.

Temiz A/B (flood 150; kurban **tek atış**, sabit 200 ms offset, her turda taze
oturum, ısıtılmış tenant, 3 tur):

| Kol | flood | **ilgisiz üçüncü şahıs** |
|---|---|---|
| **tek anahtar** | 1,91 / 1,38 / 1,45 sn | **1,60 / 1,05 / 1,14 sn** |
| **ayrı anahtar** | 0,372 / 0,370 / 0,370 sn | **0,178 / 0,178 / 0,176 sn** |

⇒ ilgisiz birinin **tek** tap gecikmesi **6–9× kötüleşiyor**. §4 ihlali değil
(§4'te erişilebilirlik maddesi yok, hiçbir kolda kayıt kaybolmadı) ve tavanlar
`ByAddress` (3000/10dk) ile `middleware.Timeout(30s)`; ama **yeni bir yüzey**, ve
mutlak bir güvenlik beyanıyla örtülemez.

⚠️ **YÖNTEM — bu ölçüm kolayca yanlış yapılır ve bir tur boyunca yanlış yapıldı.**
Tek anahtarlı flood'u **tek oturumdan** sürersen isteklerin çoğu `BySession`
300/10dk sınırına takılıp kilide **hiç dokunmadan 429** alır (yeniden üretildi:
200 istekli flood **40 ms**'de bitti, hepsi rate-limit) — sonuç "fark yok" diye
okunur. Kurbanı **döngüde** ölçmek de worst-of-N'i yavaş kola kaydırır. Doğrusu:
flood için **ayrı oturumlar**, kurban için **tek atış**.

**Veri zaten vardı.** `transactions.created_at` migration 00005'ten beri mevcut
(`NOT NULL DEFAULT now()`), `channel` da öyle. **Migration gerekmedi** — yalnız
yeni bir sorgu (`db/queries/transactions.sql` + `make sqlc`).

**Ölçülen sonuç (kural sonrası, aynı üç sonda):**

| Sonda | Önce | Sonra |
|---|---|---|
| 1+2. katman (geçmişi olan çalışan, 20 geriye tarihli POST) | 20 sayılan | **20 satır, 1 sayılan + 19 `ignored`** |
| 3. katman (1 ileri tarihli + 20 dürüst POST) | 20 `ok` | **20 satır, 0 sayılan, 20'si de `sys:person-debounce`** |
| NFC kontrolü | 1 + 19 replay-reject | **değişmedi** |

### Neden `min`, neden `max` değil, neden "yalnız sunucu bacağı" değil

- **`max` deliği korurdu.** Saldırganın beyan ettiği devasa mesafe her seferinde
  kazanırdı.
- **Yalnız sunucu bacağı**, ürünün istemciye **bilinçli olarak** bıraktığı alanı
  yok sayardı. `occurred_at`'in meşru sebebi çevrimdışı kuyruktur
  ([M9-01](../plan/m9-sonrasi.md)): **gerçek** tap zamanı `occurred_at`, yazılma
  zamanı `created_at`. `min` her iki garantiyi de korur.

## Reddedilen alternatifler

| Alternatif | Neden reddedildi |
|---|---|
| **Yalnız `created_at`'e geçmek** | `occurred_at`'i tümden yok sayardı; çevrimdışı kuyruğun (M9-01) meşru geç senkronunu duvar saati yakınlığına göre debounce ederdi. |
| **`max` kullanmak** | Deliği aynen korurdu. |
| **`created_at`'i `LastForPerson`'ın bir SÜTUNU olarak okumak** | **Denendi ve yetmedi** — bu ADR'nin ilk uygulaması buydu. 1. katmanı (mesafe) kapattı, 2. ve 3.'yü (seçim, işaret) açık bıraktı, çünkü ikisi de *hangi satırın okunduğu* ile ilgilidir, o satırın ne söylediğiyle değil. Bir denetçi ikisini de ölçtü. |
| **M5-10'a (tazelik penceresi) devretmek** | Pencere **bağlamın ömrünü** kısaltır, girdinin kusurunu düzeltmez; ve gerçek bağlayıcı zaten oturum bütçesiydi. |
| **Kabul edilen risk olarak yazmak** ([ADR 0005](0005-kabul-edilen-riskler.md)) | ADR 0005 çözümü **kırmızı çizgiyi delen** riskler içindir. Bunun çözümü bir sorgu ve birkaç satırdı, hiçbir çizgiye değmiyordu. |
| **Debounce'u tag bazlı yapmak** | §5 açıkça KİŞİ bazlı diyor: sıra hâlinde farklı kişiler aynı plakete art arda dokunabilmeli. |
| **`sys:occurred-at-bound` ile reddedilmiş satırları TEMEL olmaktan çıkarmak** | Cazip görünüyor (3. katmanı da kapatırdı) ama iki sebeple reddedildi: reddedilmiş bir tap yine de **fiziksel bir dokunuştur**, ve verdict'e göre filtrelemek çağırana bacağı boş tutmanın bir yolunu verirdi. §4.6 zaten **yazmayı** kısıtlar, **temel seçimini** değil — ve satır her hâlükârda yazılmaya devam ediyor. İşaret sorunu bunun yerine `min`'in girdisinde çözüldü. |

## Sonuçlar

**Kazanılan.** §5'in *"bir sonraki işlem yeni fiziksel dokunuş gerektirir"*
ilkesi QR'da da bir fren kazandı, ve fren **istemcinin erişemediği** bir gerçeğe
dayanıyor: ne beyan edilen zaman, ne beyan edilen sıralama, ne de ileri tarihli
bir satır onu kapatabiliyor (üçü de ölçüldü, üçü de test).

**Ne garanti ediliyor, ne edilmiyor — ölçüme indirilmiş hâliyle.**
Garanti edilen: **beyan edilen zaman, beyan edilen sıralama, ileri tarihli bir
satır ve eşzamanlılık** — dördü de tek başına debounce'u kapatamıyor; dördü de
ölçüldü ve teste bağlandı. Garanti **edilmeyen**: "hiçbir koşulda ikinci bir
sayılan kayıt olamaz". Bu ADR üç turda üç kez böyle bir cümle yazdı ve üçü de
yanlışlandı; bu yüzden artık **beşinci bir katman olmadığı iddia edilmiyor** —
yalnız dördünün kapatıldığı, nasıl ölçüldüğüyle birlikte, yazılıyor.
**Bilinen bir sınır:** serileştirme tek bir uygulama örneği ve tek bir veritabanı
varsayımı gerektirmiyor (advisory lock küme genelindedir), ama **birden fazla
veritabanı** ya da bir okuma replikası bu resmi değiştirir — bugün öyle bir kurulum
yok, ve bu ölçülmedi.

**Bedeli.** Aynı kişinin debounce penceresi içindeki tap'leri sıraya girer. İki
bilinen sonucu var:

1. **🔴 M9-01 (çevrimdışı kuyruk) BU KURALLA ÇARPIŞIR.** Telefon iki tap'i
   çevrimdışı kuyruklayıp ikisini de aynı senkronda (saniyeler arayla)
   gönderirse, ikinci satır sunucu bacağından `ignored` olur — oysa kişi
   gerçekten iki kez tap etmiştir ve `occurred_at`'leri saatler arayladır.
   Bir denetçi bunu **ölçtü** (7 sa arayla iki tap, saniyeler arayla POST →
   ikincisi `ignored`), yani bu ifşa kaçamak değil ölçülmüş bir gerçektir.
   **M9-01, kuyruk boşaltmasını sunucunun DOĞRULAYABİLECEĞİ bir işarete bağlamak
   zorundadır** (M5-10 kartı `queued` damgası + `base:queued-window`'u zaten bu
   ayrım için ayırıyor); o işaretin bulunduğu satırlarda sunucu bacağı, manuel
   istisnasıyla aynı şekilde uygulanmamalıdır. **Bu ADR o işareti tanımlamıyor.**
2. **Test kurgusu değişti.** Repodaki dört test bir günü bir saniyede simüle
   etmek için tam da bu deliği kullanıyordu (`tapAgo(900)`, `at(-300s)`), ve
   yorumları bunu açıkça söylüyordu (*"tap one declared itself 600 s ago"*).
   Artık geçmiş **beyan edilerek** değil **yazılarak** kuruluyor
   (`seedTapAgedBy` / `seedAgedRecord` / `seedRecordWithSplitClocks`, damgalar
   birlikte hareket ediyor); ölçülen tap her vakada hâlâ gerçek bir POST ve
   hiçbir iddia zayıflatılmadı. *(Bu, deliğin gerçek ve erişilebilir olduğunun
   ayrıca kanıtıdır: ürünün kendi testleri ona yaslanıyordu.)*

**Bilinen sınır 2 (ölçüldü, kapatılmadı).** Kilit anahtarını **sabitlemek**
(herkesi tek bir kilide koymak) mutasyonu **YEŞİL** kalıyor: aşırı serileştirme
hâlâ *doğrudur*, yalnız yavaştır, ve testler doğruluğu ölçüyor gecikmeyi değil.
Yani "anahtar doğru türetilmiş" iddiası testle değil **okumayla** duruyor.

**Sınır 3 DARALTILDI (kapanmadı).** `SecondsSinceLastRecordedTap` artık **debounce
penceresiyle sınırlı** (`AND created_at > clock_timestamp() - <pencere>`), çünkü
kilidin **içinde** koşuyor ve §4.6 her flood POST'una bir satır yazdırıp §4.3 o
satırları kalıcı kıldığı için taranan küme **yalnız büyüyor** — en hızlı da flood
edilen kişide. Çağıran pencerenin üstündeki değeri zaten atıyor, yani hiçbir karar
değişmiyor (mutasyonla kanıtlı: yüklemi kaldır → davranış **aynı**; pencereyi 0'a
çek → dört alt vaka **RED**). **Ölçüm** (tek çalışan, 20 002 satır): sıralama
girdisi **20 002 → 43**.
⚠️ **Süre kazancı GÜRÜLTÜ MERTEBESİNDE ve bağımsız denetim onu yeniden
üretemedi.** 10'ar koşuluk tekrar ölçümde medyan **11,92 → 10,28 ms**, ama
aralıklar örtüşüyor (öncesi min **11,13**, sonrası maks **13,07**). Erken bir
taslaktaki *"~%20 kazanç"* **geri çekilir**: tek makinede, gürültünün içinde.
**Buffer sayısı iki kolda BİREBİR aynı** (`shared hit=2109`) — yani asıl iş, yani
tarama, hiç daralmıyor.
⚠️ **Sıralamayı sınırlıyor, TARAMAYI değil:** indeks `(tenant_id, employee_id,
occurred_at)`, `created_at` içinde yok → Bitmap Heap Scan kişinin tüm geçmişini
yine geziyor ve atıyor (*"Rows Removed by Filter: 19959"*). Taramayı da sınırlamak
`created_at` üzerinde indeks, yani **migration** ister — **alınmadı**.

**🔴 DAĞITIM DEVRİ (M8).** Küme, veritabanı **ve** rol seviyesinde
`statement_timeout`, `lock_timeout` ve `idle_in_transaction_session_timeout`
**üçü de 0** (ölçüldü). Tek tavan HTTP katmanındaki `middleware.Timeout(30s)`;
yani kilit beklemesinin, sorgunun ve boşta kalan bir transaction'ın veritabanı
tarafında **hiçbir sınırı yok**. Bu bir dağıtım/konfigürasyon işidir, bu görevin
değil.

**Bilinen sınır 4 (kapatılmadı, sayıldı).** `SecondsSinceLastRecordedTap`
`(tenant_id, employee_id, created_at)` üzerinde bir indeksten faydalanırdı;
bugünkü indeks `occurred_at` üzerinde. Kişi başına satır sayısı günde onlarca
olduğu için ölçülebilir bir sorun beklenmiyor, ve indeks eklemek **migration**
demek — bilinçli olarak yapılmadı.

**Değişmeyen.** Trust formülü (§5: `20 + 50 IP + 30 GPS`) · guardrail sırası ve
metinleri · `sys:occurred-at-bound`'un 0–72 sa aralığı · `base:queued-window` ·
yön zinciri · geç kalma · şema (**migration yok**) · `channel`'ın sunucu
türetimli oluşu.

**Kanıt.**
`internal/domain/tap/decide_test.go` —
`TestDecide_DebounceMeasuresBothTheDeclaredAndTheServerClock` (8 vaka, üç katman)
ve `TestDebounceGap_TakesTheSmallerDistance` (8 vaka, fonksiyonun kendisi).
`internal/handler/qr_db_test.go` —
`TestQRDB_ABackdatedOccurredAtNoLongerDefeatsTheDebounce` (geçmişi **olan**
çalışan + ileri tarihli zehir + NFC kontrolü) ve
`TestQRDB_AManualPredecessorDoesNotDebounceARealTap` (istisna, iki yönlü).
**Mutasyonlar, hepsi RED:** sunucu bacağını kaldır (7 alt vaka) · beyan bacağını
kaldır (4) · sunucu bacağında `min`→`max` (9) · negatif beyanı sıfıra çek (4) ·
`gather`'da yeni gerçeği düşür (2) · sorguyu `ORDER BY occurred_at`'e çevir ·
sorgudan manuel yüklemini kaldır.
