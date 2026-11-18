# Taptime entegrasyon API'si — entegrasyon rehberi

Rehber 5. baskı (2026-11-15), API sürüm 1 için. Bu rehberi API'nin kendisi, anahtarsız,
`GET /v1/guide.tr.md` adresinde sunar; asıl metin olan İngilizcesi — bölüm bölüm aynı belge —
`GET /v1/guide.md` adresindedir. Makinenin okuduğu sözleşme `GET /v1/openapi.yaml` adresindeki
OpenAPI 3.1 belgesidir; bu rehber onu açıklar ve ikisinin aynı şeyi söylediği her yerde ikisi
birbirini tutar.

Bu rehberdeki her örnek cevap, API'nin verdiği bir cevaptır: hepsi 4. ile 10. bölümlerde anlatılan,
gerçek servise karşı koşturulmuş tek bir hikâyeden gelir.

<a id="contents"></a>
## İçindekiler

- [1. Genel bakış](#overview)
- [2. Kimlik doğrulama ve anahtarlar](#auth)
- [3. Ortak sözleşme](#common)
- [4. Lokasyonlar](#locations)
- [5. Çalışanlar ve aktivasyon](#employees)
- [6. Punch akışı](#feed)
- [7. Geçmiş sorgusu](#history)
- [8. Rehber (directory)](#directory)
- [9. Çalışma süresi çizelgesi](#timesheets)
- [10. Webhook'lar](#webhooks)
- [11. Askıya alma](#suspension)
- [12. Sözleşme farkları](#differences)
- [13. OAuth ile bağlanan uygulamalar](#oauth)
- [14. Bu rehberdeki değişiklikler](#changes)

<a id="overview"></a>
## 1. Genel bakış

<a id="overview-what"></a>
### 1.1 API ne yapar

Taptime çalışma süresini dokunuşla kaydeder: çalışan, bir lokasyona monte edilmiş plakete kendi
telefonuyla dokunur. Entegrasyon API'si bir işletmenin İK ya da ERP sistemine şunları sağlar:

- işletmenin **lokasyonlarını** ve **çalışanlarını** yazmak (`PUT`, `DELETE`) — Taptime sizin
  sisteminizin bildiği yerleri ve kişileri, sizin referanslarınızla bilsin;
- işletmenin **punch**'larını hiçbirini kaybetmeyen bir akıştan okumak (`GET /v1/punches`) ya da bir
  zaman aralığını okumak (geçmiş sorgusu);
- işletmenin bilebileceği çalışanların **rehberini** okumak (`GET /v1/directory`);
- işletmenin kendi çalışanlarının bir bordro dönemindeki **sayılan çalışma süresini** okumak —
  Taptime'ın kendi raporunun rakamları (`GET /v1/timesheets`);
- her punch'ı olduğu anda **webhook** ile almak.

Bir istek **yalnız tek bir işletmeyi — anahtarın ait olduğu işletmeyi —** okur ve yazar. Hiçbir
istek bir işletmenin adını vermez ve hiçbir parametre bir anahtarın erişimini genişletmez.

<a id="overview-companies"></a>
### 1.2 İşletmeler, şirketler ve gruplar

Taptime'da her şirket kendi **işletmesidir**: kendi anahtarı, kendi akışı, kendi webhook'u ve kendi
şirket kodu (punch'ın `company` alanı, ör. `KF` ya da `KM`) vardır. Şirket kodunu Taptime, işletme
için API'yi açarken yazar.

İki işletme tek bir **şirketler grubuna** (holding) ait olabilir (grubu Taptime kurar). Bir grupta:

- bir şirketin çalışanı öbür şirketin plaketlerinde punch yapabilir; tersi de;
- böyle bir punch **işverene aittir** — onu işverenin kuralları karara bağlar, onay gerekiyorsa
  işverenin bir yöneticisi onaylar, düzeltmeler işverende yapılır;
- işverenin akışında (`employer` kapsamı) **ve** plaketine dokunulan şirketin akışında (`location`
  kapsamı) bulunur, ikisinde de aynı `id` ile — bkz. [bölüm 6.2](#feed-scopes);
- her şirketin rehberi öbür şirketin çalışanlarını da listeler — bkz. [bölüm 8](#directory).

Bu rehberin hikâyesinde tek gruba ait iki şirket vardır: plaketi `KF-VLT` ("Valletta")
lokasyonunda olan **KF** ve departmanları lokasyonları olan **KM**: Taptime'da **KM'nin bir
departmanı bir lokasyondur** ve plaketi oraya monte edilir (`DEP-1`, "Production"). KF'nin
Valletta'sında vardiya yapan bir KM çalışanı orada KF'nin plaketine dokunur.

<a id="overview-host"></a>
### 1.3 İstekler nereye gönderilir

API'nin kendine ait bir host'u vardır: OpenAPI belgesindeki `servers` adresi. Her yol `/v1`
altındadır. Aşağıdaki örnekler bu adresi `$TAPTIME_API`, anahtarı `$TAPTIME_KEY` olarak yazar;
bunlar yer tutucudur, değer değildir. Taptime'ın başka hiçbir host'unda `/v1` yoktur (404) ve API
host'u `/v1` dışında hiçbir şeye cevap vermez (404). API çerez koymaz, çerez okumaz. OAuth ile
bağlanan bir uygulama üç uç için Taptime'ın kendi adresini de kullanır — bkz. [bölüm 13](#oauth).

<a id="overview-sandbox"></a>
### 1.4 Sandbox

Sandbox, bir entegrasyonu denemek için ayrı bir Taptime'dır: kendi alan adı, sunucunun kendi
derlemesi, kendi veritabanı ve kendi operatörü vardır. İçinde üretimin hiçbir işletmesi, anahtarı ya
da punch'ı yoktur ve içindeki hiçbir şey üretime ulaşmaz. **Alan adı henüz seçilmedi**: seçildiğinde
bu bölüm sandbox'ın host'larını yazacak. O zamana kadar, Taptime'ın bu amaçla açtığı bir işletmeye
karşı test edin.

- **Anahtarlar `tt_test_` ile başlar** ve sandbox'ın Taptime panelinde üretilir. Bir `tt_test_`
  anahtarı üretimde (401), bir `tt_live_` anahtarı sandbox'ta (401) reddedilir; hiçbir şeye
  bakılmadan önce.
- **API'yi orada da Taptime açar**, işletme işletme ([2.1](#auth-enable)): sandbox'ın operatörü
  açana kadar geçerli bir anahtar 403 `api_disabled` alır.
- **Sandbox'tan e-posta çıkmaz**: aktivasyon bağlantısı sandbox'ın panelinde gösterilir, gönderilmez.
- **Bu rehberdeki her şey orada da geçerlidir**, bir ekle: POST /v1/sandbox/taps bir çalışanın bir
  lokasyonun plaketine dokunuşunu simüle eder, çünkü sandbox'ta ne duvar ne telefon vardır. Kendi
  OpenAPI belgesi sandbox'ın API host'unda GET /v1/sandbox/openapi.json'dur.

Gövde çalışanı ve lokasyonu sizin referanslarınızla, kanalı da (`nfc` ya da `qr`) adlandırır:

```sh
curl -sS -X POST "$TAPTIME_API/v1/sandbox/taps" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  --data-binary '{"employeeRef":"E-1042","locationRef":"KF-VLT","channel":"nfc"}'
```

Plaketten sonrası Taptime'ın kendi dokunuşudur: plaketin imzalı okuması doğrulanır, sayacı
ilerletilir, kurallar karar verir, kayıt yazılır ve oluşan punch akışta yer alır ve webhook'unuza
gönderilir. Cevap (`201`) `verdict`'i (`ok`, `flag`, `reject` ya da `ignored`), akışın ve webhook'un
onun için taşıdığı `punchId`'yi ve dokunuşun şu an bir punch olup olmadığını (`inFeed`) taşır.
`Idempotency-Key` her yazmada olduğu gibi çalışır ([3.3](#idempotency)).

- API'den açılan bir lokasyonun, sahibi panelde girene kadar adres aralığı ya da koordinatı yoktur;
  bu yüzden oradaki dokunuşlar `flag` olur ve onay bekler ([4](#locations)). İsteğin kendi adresi
  dokunuşun adresidir ve GPS yoktur: `ok` görmek için lokasyona çağıranınızın adres aralığını verin.
- Aynı kişinin 60 saniye içindeki tekrarı, üretimde olduğu gibi `ignored` olur ([6.1](#feed-what)).
- Bir lokasyon sanal plaketini kendi işletmesinin oradaki ilk simüle dokunuşunda alır. Grubunuzun
  öbür şirketinin bir lokasyonuna ([1.2](#overview-companies)) çalışanınız — grup dokunuşu — o şirket
  orada kendisi bir dokunuş simüle ettikten sonra dokunabilir: sandbox hiçbir zaman başka bir
  işletmede plaket üretmez. Grup dokunuşunun kendisi gerçeği gibi kaydedilir: o şirketin plaket
  sayacını ilerletir ve onun lokasyon akışında görünür.

<a id="overview-scope"></a>
### 1.5 API'nin yapmadıkları (sürüm 1)

- Punch yazmaz, punch iptal etmez ya da düzeltmez: düzeltmeler işveren tarafından Taptime
  panelinde yapılır ve bir v1 punch'ı hiç değişmez (`version` hep `1` kalır).
- Departmanlar için ayrı uçları yoktur (KM'nin bir departmanı bir lokasyondur) ve bir lokasyonu
  silemez.
- Webhook'u yönetmez: uç nokta, adresi ve imza anahtarı işletme sahibinindir, Taptime panelindedir.
- Lokasyonun kendi saat dilimi yoktur: her lokasyonun saat dilimi işletmeninkidir.
- Bir ortak şirket, başka bir şirketin çalışanının punch'ını onaylayamaz.
- Kimlik bilgileri işletmenin anahtarları ve bir işletmenin OAuth ile bağladığı uygulamaların erişim
  tokenlarıdır ([bölüm 13](#oauth)); başkası yoktur.

<a id="auth"></a>
## 2. Kimlik doğrulama ve anahtarlar

<a id="auth-enable"></a>
### 2.1 API'yi Taptime açar

API bir işletme için ancak Taptime onu o işletme için açtıktan (ve şirket kodunu yazdıktan) sonra
çalışır. O zamana kadar — ve Taptime kapatırsa yeniden — geçerli anahtarlı her istek
**403 `api_disabled`** alır ve işletmenin webhook'u hiçbir şey göndermez; işletmenin punch'ları
akışında kaydedilmeye devam eder. API kapalıyken işletme sahibi anahtar oluşturamaz.

**OAuth ile bağlanan bir uygulama ([bölüm 13](#oauth)) bu adımı gerektirmez**: erişim tokenı, API'sini
Taptime'ın hiç açmadığı bir işletme için de çalışır. Taptime bir işletmenin API'sini kapatmışsa bir token
da **403 `api_disabled`** alır ([13.9](#oauth-api)).

<a id="auth-keys"></a>
### 2.2 Anahtarlar

Anahtarları işletmenin **sahibi** Taptime panelinde, **API keys** bölümünde oluşturur. Bir anahtar
`tt_live_` ve ardından 43 base64url karakterdir; **bir kez**, oluşturulduğunda gösterilir, bir daha
asla. Panel her anahtarı adıyla, ilk karakterleriyle ve en son ne zaman kullanıldığıyla listeler.
Sahip tek bir anahtarı ya da bütün anahtarları birden iptal edebilir.

Anahtarı her istekte (üç belge hariç) şöyle gönderin:

```http
Authorization: Bearer <key>
```

- Başka biçimdeki bir anahtar, hiçbir şeye bakılmadan reddedilir: **401 `unauthorized`**.
- Bilinmeyen bir anahtar ile iptal edilmiş bir anahtar **aynı** 401'i alır,
  `WWW-Authenticate: Bearer realm="taptime"` ile. Bir iptal ve Taptime'ın API'yi kapatması bir
  sonraki istekte geçerlidir: hiçbir şey önbelleğe alınmaz.
- Anahtarı log'lardan, URL'lerden ve `Authorization` dışındaki başlıklardan uzak tutun.
- **`X-Request-Id`'ye asla bir anahtar ya da başka bir sır koymayın.** Taptime gönderdiğiniz
  `X-Request-Id`'yi isteğin kimliği olarak log'larına yazabilir: her karakteri `A`–`Z`,
  `a`–`z`, `0`–`9`, `-`, `_` ve `.` kümesinden olan, 1 ile 64 karakter arası ve `tt_live_`,
  `tt_test_`, `tt_whsec_`, `tt_cs_`, `tt_ac_`, `tt_at_` ve `tt_rt_`'nin hiçbirini içermeyen (harf büyüklüğüne bakılmadan) bir değer. Böyle
  bir değer olduğu gibi yazılır — öneki olmadan bir anahtarın karakterleri dahil. Başka her değer —
  daha uzun, başka bir karakterli ya da bu öneklerden birini taşıyan — Taptime'ın kendi kimliğiyle
  değiştirilir: kimliğinizi Taptime desteğiyle eşleştirmek istiyorsanız kümeye ve uzunluğa uyun.

Bir işletmenin birden çok anahtarı olabilir (sistem başına bir anahtar iyi bir kuraldır). Her
anahtarın kendi istek bütçesi, kendi pasifleştirme bütçesi ve kendi Idempotency-Key'leri vardır —
bkz. [bölüm 3.3](#idempotency) ve [bölüm 3.4](#rate-limits).

<a id="auth-business"></a>
### 2.3 İşletme

`GET /v1/business` anahtarın — ya da OAuth tokenının, [bölüm 13.9](#oauth-api) — adına iş yaptığı işletmeyi döndürür: `id` (`biz_` ve bir UUID), `name` ve
`companyCode` — Taptime'ın API'yi açarken verdiği şirket kodu, bir punch'ın `company` olarak taşıdığı
değerin aynısı. Sorgu parametresi almaz ve işletme askıdayken de açıktır.

<a id="common"></a>
## 3. Ortak sözleşme

**Sürüm 1 içinde yanıtlara ve webhook gövdelerine alan eklenebilir: tanımadığınız alanı yok sayın.**
Bir alanı silmek, adını değiştirmek ya da bir değerin anlamını değiştirmek yeni bir sürümdür. İstek
gövdeleri bunun tersidir: bir ucun almadığı alan reddedilir ([3.6](#bodies)).

<a id="errors"></a>
### 3.1 Hatalar

Her ret tam üç alanlı bir JSON gövdesidir:

<!-- error: BadRequest/limit -->
```json
{"code": "invalid_request", "message": "limit must be a whole number from 1 to 1000.", "details": {"max": 1000, "parameter": "limit"}}
```

- `code` makinenin okuduğu cevaptır — aşağıdaki kodlardan biri, başka hiçbir şey.
- `message` bir insan için yazılmış (İngilizce) bir cümledir. İsteğin gönderdiği bir değeri asla
  tekrar etmez.
- `details` her zaman bir nesnedir, boş olabilir; anahtarları kodlardan sonra listelenmiştir.

OpenAPI belgesinin her işlemi, cevaplayabildiği kodları tam olarak `x-error-codes` içinde listeler.

| Kod | Durum | Anlamı |
|---|---|---|
| `invalid_request` | 400 | Sözleşmenin reddettiği bir parametre, başlık ya da gövde. |
| `body_too_large` | 400 | İstek gövdesi 65536 bayttan büyük. |
| `unauthorized` | 401 | Anahtar ya da erişim tokenı yok ya da geçerli değil (süresi dolmuş ya da iptal edilmiş bir token dahil). |
| `api_disabled` | 403 | API bu işletme için açık değil. |
| `tenant_suspended` | 403 | İşletme askıda ve bu değişiklik kabul edilmiyor. |
| `insufficient_scope` | 403 | OAuth ile bağlanmış bir uygulama, bağlantısının kapsamadığı bir rotayı çağırdı (`details.scope`). Anahtar bu kodu hiç almaz. |
| `not_found` | 404 | Böyle bir kaynak yok (ayrıca: bilinmeyen bir yol ya da yolun almadığı bir yöntem). |
| `email_unavailable` | 409 | İşletmenin başka bir aktif çalışanı bu e-posta adresine sahip. |
| `already_activated` | 409 | Çalışan zaten aktive olmuş. |
| `idempotency_key_reused` | 422 | Idempotency-Key başka bir istekle kullanılmış. |
| `validation_failed` | 422 | Sözleşmenin reddettiği bir gövde alanı ya da yolun referansı. |
| `timezone_mismatch` | 422 | Lokasyonun saat dilimi işletmeninki olmalı. |
| `rate_limited` | 429 | Çok fazla istek; `Retry-After` saniye bekleyin. |
| `deactivation_budget_exceeded` | 429 | Bu anahtar saatlik pasifleştirme bütçesini doldurdu; `Retry-After` saniye bekleyin. |
| `unavailable` | 503 | İstek Taptime tarafında tamamlanamadı; yeniden deneyin. |

`details`'in anahtarları:

| Anahtar | Nerede |
|---|---|
| `field` | `validation_failed`, `timezone_mismatch`: hatalı gövde alanı ya da yolun referansı için `ref`. |
| `reason` | Neden — bir sonraki tabloya bakın. |
| `parameter` | Hatalı sorgu parametresi (`limit`, `cursor`, `scope`, `updatedSince`). |
| `parameters` | Birlikte hatalı sorgu parametreleri (`from` ve `to`). |
| `allowed` | Ucun kabul ettiği adlar. |
| `header` | Hatalı başlık (`Content-Encoding`, `Idempotency-Key`). |
| `max` | En büyük `limit`. |
| `maxBytes` | En büyük gövde. |
| `maxHours` | En uzun geçmiş aralığı, saat olarak. |
| `maxDays` | En uzun çizelge dönemi, gün olarak. |
| `retryAfterSeconds` | `Retry-After` ile aynı. |
| `businessTimezone` | İşletmenin kendi saat dilimi (`timezone_mismatch`). |
| `scope` | Rotanın gerektirdiği OAuth kapsamı (`insufficient_scope`). |

`reason`'ın değerleri:

| Neden | Kod | Anlamı |
|---|---|---|
| `required` | `validation_failed` | Zorunlu bir alan eksik (ya da resend-activation'da çalışanın adresi yok). |
| `type` | `validation_failed` | Bir alanın JSON türü yanlış. |
| `invalid` | `validation_failed` | Bir alanın değeri kuralının dışında (bir uzunluk, bir adres, bir referansın kalıbı). |
| `mismatch` | `validation_failed` | Gövdedeki `externalRef`, yoldaki referans değil. |
| `unknown_field` | `validation_failed` | Gövde, ucun almadığı bir alan adlandırıyor. |
| `duplicate_field` | `validation_failed` | Gövde aynı alanı iki kez adlandırıyor. |
| `hidden_character` | `validation_failed` | Yeni ya da değişen bir ad görünmez ya da kontrol karakteri taşıyor. |
| `administrator_address` | `validation_failed` | E-posta adresi işletmenin bir yöneticisinin. |
| `malformed_json` | `invalid_request` | Gövde tek bir JSON nesnesi değil. |
| `invalid_utf8` | `invalid_request` | Gövde UTF-8 değil ya da bir dize eşsiz bir surrogate kaçışı taşıyor. |
| `unencoded_plus` | `invalid_request` | Bir anın `+`'sı kodlanmadan gönderilmiş (boşluk olarak gelir). |

Bir **503 `unavailable`** her zaman Taptime'ın hatasıdır, sizin değil: kısa ve giderek uzayan bir
beklemeyle yeniden deneyin. Aynı Idempotency-Key ile yeniden denenen bir yazma güvenlidir (başarısız
bir yazma hiçbir şey kaydetmemiştir). 30 saniye içinde cevaplanmayan bir istek 503 alır.

<a id="paging"></a>
### 3.2 Sayfalar ve `limit`

Her liste sayfalıdır. `limit` 1 ile 1000 arasında bir tam sayıdır; verilmezse bir sayfa en çok 500
kayıt taşır. Başka her şey — 0, 1001, bir işaret, bir kesir — **400 `invalid_request`** alır,
`details.parameter` `limit` ile; ikinci bir `limit` tekrarlanan bir parametredir ve öyle cevaplanır
([3.7](#query)). Her listenin kendi imleci ve son sayfa
için kendi kuralı vardır; listelerle birlikte anlatılmıştır ([5.6](#employees-read),
[6.4](#feed-paging), [7](#history), [8.2](#directory-walk), [9.3](#timesheets-paging)).

<a id="idempotency"></a>
### 3.3 Idempotency-Key

Her yazma (`PUT`, `DELETE`, `POST`) bir `Idempotency-Key` başlığı taşıyabilir: 1 ile 255 arası
görünür ASCII karakterden oluşan tek bir değer (ikinci bir başlık, bir boşluk ya da ASCII olmayan
bir karakter 400 alır, `details.header` `Idempotency-Key` ile). Bir yazmayı 24 saat içinde aynı
anahtarla tekrarlarsanız:

- **aynı istek**, iş yeniden yapılmadan, ilk isteğin durumuyla ve `Idempotent-Replayed: true`
  başlığıyla cevaplanır. Gövde kaynağın **şimdiki hâlidir** (arada yapılan bir yeniden adlandırma
  görünür); kaynak artık yoksa replay 404 `not_found` alır;
- **farklı bir istek** **422 `idempotency_key_reused`** alır;
- aynı anahtarla aynı anda gelen iki istek sırayla cevaplanır.

"Aynı istek" **bayt bayt** aynıdır: yöntem, yol tam gönderildiği gibi (yüzde kodlaması dahil)
ve gövdenin baytları — bir yazma sorgu dizgesi almaz ([3.7](#query)). JSON'da başka boşluklar
**farklı** bir istektir — 422. Başlıklar bunun **parçası değildir**: aynı
anahtarla farklı bir `Content-Type` ya da `User-Agent` bir replay'dir. Bir referansın iki yazımı
(`%41BC` ve `ABC`) tek bir lokasyondur ama iki istektir.

Idempotency-Key'ler entegrasyon anahtarı başına tutulur: bir işletmenin iki anahtarı bunları
paylaşmaz. Başarısız olan bir yazma (herhangi bir 4xx ya da 503) hiçbir şey kaydetmemiştir — anahtarı
yeniden kullanılabilir.

<a id="rate-limits"></a>
### 3.4 Oran sınırları

| Sınır | Değer |
|---|---|
| İstemci adresi başına dakikada istek | 1200 |
| Anahtar başına dakikada istek | 600 |
| Anahtar başına saatte pasifleştirme | 20 |
| OAuth bağlantısı başına dakikada istek | 600 |
| OAuth uygulaması başına dakikada istek, bütün bağlantılarında | 6000 |

Adres sınırı bir adresten gelen istekleri — üç belgeyi ve anahtarı doğru biçimli her isteği —
anahtara daha bakılmadan sayar; anahtar sınırı bir anahtarın her adresten gelen isteklerini sayar. Birini aşınca: **429
`rate_limited`**, `Retry-After` ile (saniye; bugün dakikanın tamamı). Başka hiçbir oran sınırı
başlığı gönderilmez. Pasifleştirmelerin kendi bütçesi vardır — bkz. [bölüm 5.5](#employees-deactivate).
Bir OAuth erişim tokenı da adres bütçesine sayılır ve kabul edildiğinde geri verilir; ardından onu
bağlantının ve uygulamanın bütçeleri sayar — bkz. [bölüm 13.9](#oauth-api).

<a id="limits"></a>
### 3.5 Sınırlar bir bakışta

Bunlar OpenAPI belgesinin `info.x-limits`'idir ve servisin kendi değerleridir.

| Anahtar | Değer | Ne |
|---|---|---|
| `requestsPerAddressPerMinute` | 1200 | İstemci adresi başına dakikada istek. |
| `requestsPerKeyPerMinute` | 600 | Anahtar başına dakikada istek. |
| `requestsPerGrantPerMinute` | 600 | OAuth bağlantısı başına dakikada istek. |
| `requestsPerAppPerMinute` | 6000 | OAuth uygulaması başına dakikada istek, bütün bağlantılarında. |
| `deactivationsPerKeyPerHour` | 20 | Anahtar başına saatte pasifleştirme. |
| `maxBodyBytes` | 65536 | En büyük istek gövdesi. |
| `defaultPageSize` | 500 | `limit`'siz bir sayfa. |
| `maxPageSize` | 1000 | En büyük `limit`. |
| `idempotencyHours` | 24 | Bir Idempotency-Key'in ne kadar tutulduğu. |
| `requestTimeoutSeconds` | 30 | O zamana kadar cevaplanmayan istek 503 alır. |
| `feedWindowPositions` | 10000 | Bir sayfanın okuduğu en çok akış konumu. |
| `historyMaxHours` | 745 | En uzun geçmiş aralığı (31 gün ve bir saat). |
| `timesheetMaxDays` | 31 | En uzun çizelge dönemi, gün olarak. |
| `directoryOverlapMinutes` | 10 | Rehberin `updatedSince`'ten ne kadar önceden okuduğu. |
| `webhookTimeoutSeconds` | 10 | Bir webhook denemesinin cevabı ne kadar beklediği. |
| `webhookAttempts` | 6 | Bir webhook tesliminin deneme sayısı. |
| `webhookTestMessagesPerHour` | 10 | İşletme başına saatte test mesajı ve "send again now". |
| `invitesPerBusinessPerHour` | 50 | Bir işletmenin saatte gönderdiği aktivasyon e-postası. |
| `invitesPerBusinessPerDay` | 300 | Bir işletmenin günde gönderdiği aktivasyon e-postası. |
| `invitesPerEmployeePerHour` | 3 | Bir çalışana saatte gönderilen aktivasyon e-postası. |
| `invitationQueueHours` | 72 | Kuyruktaki bir aktivasyon e-postasının en çok bekleme süresi. |
| `invitationSendAttempts` | 5 | Bir aktivasyon e-postasının deneme sayısı. |

<a id="bodies"></a>
### 3.6 İstek gövdeleri

- En çok **65536 bayt**: daha büyük bir gövde **400 `body_too_large`** alır (`details.maxBytes`).
  Sıkıştırılmış ya da başka biçimde kodlanmış bir gövde (`identity` dışında bir `Content-Encoding`)
  400 alır.
- **UTF-8**, tek bir **JSON nesnesi**: başka her şey 400 `invalid_request` alır (`reason`
  `malformed_json`; UTF-8 olmayan bir gövde ya da `\ud800` gibi eşsiz bir surrogate kaçışı taşıyan bir
  dize için `invalid_utf8`).
- Tam olarak belgelenen alanlar, **her biri en çok bir kez**, tam yazıldığı gibi (`Name`, `name`
  değildir): başka her alan **422 `validation_failed`** alır, `reason` `unknown_field` ve kabul
  edilen adlar `details.allowed` içinde; iki kez adlandırılan bir alan 422 `duplicate_field` alır —
  son değer kazanmaz.
- `Content-Type: application/json` gönderin.

<a id="query"></a>
### 3.7 Sorgu parametreleri

Her `GET` tam olarak kendi belgelenmiş parametrelerini alır, her biri en çok bir kez, tam yazıldığı
gibi: başka her parametre (`Scope`, `curser`), tekrarlanan bir parametre ve ayrıştırılamayan bir
sorgu dizgesi (`%zz`, parametreler arasında bir `;`) **400 `invalid_request`** alır, kabul edilen
adlar `details.allowed` içinde. Yanlış yazılmış bir parametre asla yok sayılmaz.
`GET /v1/employees/{ref}` ve üç belge hiç parametre almaz: her biri 400 alır, `details.allowed` boş.
Bir yazma (`PUT`, `DELETE`, `POST`)
**hiç sorgu dizgesi almaz**: `?`'den sonraki her şey — bir parametre, ayrıştırılamayan bir parametre,
boş bir `&` — **400 `invalid_request`** alır, `details.allowed` boş, ve hiçbir şey yazılmaz. `dryRun`
ya da benzeri bir şey yoktur: `?dryRun=1` ile gönderilen bir `PUT` reddedilir, yapılmaz. Reddedilen
bir yazma Idempotency-Key'i harcamaz ([3.3](#idempotency)): aynı anahtarı sorgusuz yeniden gönderin.

Sorgu dizgesindeki anlar (`from`, `to`, `updatedSince`) RFC 3339'dur —
`YYYY-MM-DDThh:mm:ss`, isteğe bağlı olarak noktadan sonra bir kesir (`.fraction`), ardından `Z` ya da bir saat
farkı `±hh:mm` — büyük harf `T` ve `Z` ile ve en çok mikrosaniye hassasiyetle (daha ince bir an
reddedilir, yuvarlanmaz). **Saat farkının `+`'sını `%2B` olarak kodlayın**: kodlanmazsa boşluk
olarak gelir ve cevap `reason` `unencoded_plus` ile 400'dür. Çizelgenin `from` ve `to`'su an değil
takvim günüdür, `YYYY-MM-DD` ([9.1](#timesheets-period)).

<a id="identifiers"></a>
### 3.8 Kimlikler, referanslar ve zamanlar

- Taptime'ın kimlikleri: `emp_` (çalışan), `loc_` (lokasyon), `pch_` (punch) ve `dlv_` (webhook
  teslimi) ve ardından bir UUID; `tag_` ve ardından plaketin 14 onaltılık karakteri. Bunları
  içini yorumlamadığınız dizeler olarak saklayın.
- **Sizin referanslarınız** (`externalRef`, bir yolun `{ref}`'i) `A-Z`, `a-z`, `0-9`, `.`, `_`,
  `~` ve `-` karakterlerinden 1 ile 64 arası, bir harf ya da rakamla başlayan karakterdir. Yolda bir
  referans yüzde kodlanabilir: **bir kez** çözülür ve sonra kalıba uymalıdır (`%41BC`, `ABC`'dir;
  kodlanmış bir `/`, bir kontrol karakteri ya da `%2541` gibi çift kodlama `field` `ref` ile 422
  alır).
- API'nin cevapladığı her an **UTC**'de RFC 3339'dur. Bir işletmenin saat dilimi raporları ve
  çizelgesinin günleri için önemlidir ([9.1](#timesheets-period)), hiçbir an için değil.
- Değeri olmayan bir alan `null`'dır, asla eksik değildir.

<a id="headers"></a>
### 3.9 Cevap başlıkları

Her `/v1` cevabı `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` ve
`Referrer-Policy: no-referrer` taşır — saklanıp yeniden doğrulanabilen iki rehber hariç
(`Cache-Control: no-cache`, bir `ETag`, eşleşen bir `If-None-Match`'te 304). Bir 429
`Retry-After`, bir 401 `WWW-Authenticate`, replay edilen bir yazma `Idempotent-Replayed: true`
taşır.

<a id="locations"></a>
## 4. Lokasyonlar

`PUT /v1/locations/{ref}` işletmenin lokasyonlarından birini sizin referansınızla oluşturur ya da
günceller.

| Alan | Tür | |
|---|---|---|
| `name` | dize, zorunlu | Çevresindeki boşluklar atıldıktan sonra 1 ile 80 karakter. |
| `active` | mantıksal, zorunlu | Yalnız bir etiket — aşağıya bakın. |
| `timezone` | dize, isteğe bağlı | Gönderilirse işletmenin saat dilimi olmalıdır. |
| `externalRef` | dize, isteğe bağlı | Gönderilirse yoldaki referans olmalıdır. |

- Bilinmeyen bir referans lokasyonu **oluşturur**: **201**. Bilinen bir referans yeniden
  adlandırılır ya da yeniden etiketlenir: **200**. Lokasyonu olduğu gibi tarif eden bir istek hiçbir
  şeyi değiştirmez: 200 ve hiçbir şey yazılmaz.
- Bir `PUT` lokasyonun tamamını gönderir: her seferinde `name` ve `active`. İki isteğe bağlı alanı
  kendine ait bir şey tutmaz — `timezone` işletmeninkidir, `externalRef` yolunki — bu yüzden
  gönderilmemeleri hiçbir şeyi değiştirmez.
- Bir lokasyon **asla adıyla eşleştirilmez**. Taptime'da zaten var olan (panelde kurulmuş) bir
  lokasyona API'den ancak sahibi Taptime panelinde onun **External ref**'ini doldurduktan sonra
  ulaşılır; o zamana kadar aynı referans ikinci bir lokasyon oluşturur.
- **`timezone`**: lokasyonun kendi saat dilimi yoktur. İşletmeninki olmayan bir `timezone`
  **422 `timezone_mismatch`** alır ve `details.businessTimezone` işletmeninkini söyler.
- **`active` bir etikettir.** Saklanır ve cevaplanır; o lokasyondaki dokunuşların nasıl karara
  bağlandığını değiştirmez — pasif bir lokasyonun dokunuşları aktif olanınki gibi karara bağlanır.
- **API'nin oluşturduğu bir lokasyondaki dokunuşlar onay bekler.** Bir dokunuşun o lokasyonda
  yapıldığının kanıtı — ağ adres aralıkları ve koordinatı — yalnız Taptime panelinde yazılır, API
  üzerinden asla. Yazılana kadar oradaki her dokunuş kaydedilir ve **bir yöneticinin onayını
  bekler**; akışa yalnız onaylanırsa girer ([bölüm 6.1](#feed-what)).
- **Adlar.** Emoji, bayraklar ve her yazının harfleri kabul edilir. Yumuşak tire, U+200E, U+200F
  ve U+061C yön işaretleri ve iki uçtaki bayt sırası işareti sessizce atılır. **Yeni ya da değişen**
  bir ad kontrol karakteri, satır ya da paragraf ayırıcısı, yön geçersiz kılma ya da yalıtımı
  (U+202A–U+202E, U+2066–U+2069), sıfır genişlikli boşluk (U+200B), kelime birleştirici (U+2060) ya
  da içte bir U+FEFF taşıyamaz; iki görünür karakter arasında olmayan bir birleştirici (U+200C,
  U+200D) ya da bir bayrağın dışında bir etiket karakteri de taşıyamaz: 422 `validation_failed`,
  `reason` `hidden_character` (uzunluk reddi `invalid`'dir). Kayıtlı bir ad olduğu gibi kabul
  edilir.
- İşletme askıdayken bu uç 403 `tenant_suspended` alır ([bölüm 11](#suspension)).

Hikâyede KF lokasyonunu yazar:

```sh
curl -sS -X PUT "$TAPTIME_API/v1/locations/KF-VLT" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: loc-KF-VLT-v1" \
  --data-binary '{"name":"Valletta","active":true,"timezone":"Europe/Malta"}'
```

<!-- example: putLocation 201 -->
```json
{
  "id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd",
  "externalRef": "KF-VLT",
  "name": "Valletta",
  "timezone": "Europe/Malta",
  "active": true,
  "createdAt": "2026-10-16T09:13:12.861739Z"
}
```

KM, `DEP-1` ("Production") departmanını aynı biçimde yazar: KM'nin bir departmanı bir lokasyondur.

<a id="employees"></a>
## 5. Çalışanlar ve aktivasyon

<a id="employees-put"></a>
### 5.1 Çalışan oluşturmak ve güncellemek

`PUT /v1/employees/{ref}` sizin referansınızla bir çalışanı oluşturur, günceller ya da pasifleştirir.

| Alan | Tür | |
|---|---|---|
| `firstName` | dize, zorunlu | |
| `lastName` | dize, zorunlu | `firstName` ile birlikte en çok 120 karakter; boşluklar birleştirilir. |
| `email` | dize ya da `null`, zorunlu | Aktivasyon e-postasının adresi; yoksa `null`. Anahtar bulunmalıdır. |
| `active` | mantıksal, zorunlu | `false` pasifleştirir ([5.5](#employees-deactivate)). |
| `employeeCode` | dize ya da `null`, isteğe bağlı | 1 ile 64 karakter. Gönderilmezse `null` olur. |
| `externalRef` | dize, isteğe bağlı | Gönderilirse yoldaki referans olmalıdır. |

- **Bilinmeyen bir referans önce** işletmenin Taptime'da zaten olan bir çalışanını **bağlar**:
  pasifleştirilmemiş, referansı olmayan ve gönderilen e-posta adresine sahip bir çalışanı (harf
  büyüklüğüne bakılmadan karşılaştırılır; yalnız bu işletmede, asla bir ortak şirkette). Bağlama
  **200** alır. Kayıtlı adres yalnız harf büyüklüğünde farklıysa bağlamanın ardından bir adres
  değişikliği gelir.
- Aksi hâlde referans yeni bir çalışan **oluşturur**: **201** ve `email` bir adresse bir aktivasyon
  e-postası **kuyruğa girer** ([5.2](#employees-activation)).
- Bilinen bir referans güncellenir: **200**. Çalışanı olduğu gibi tarif eden bir istek hiçbir şeyi
  değiştirmez: 200.
- **Bir `PUT` yerine koyar, yamalamaz**: gönderilmeyen bir `employeeCode` `null` olur.
  `employeeCode`'u `77` olan bir çalışan `employeeCode` olmadan yazılırsa `employeeCode: null` ile
  cevaplanır — sonraki punch'ları ve bir ortak şirketin rehberi de `null` taşır. Tuttuğunuz her
  alanı her seferinde gönderin. (Öteki isteğe bağlı alan `externalRef`'in kendine ait bir değeri
  yoktur: gönderilmezse yoldaki referanstır.)
- **Adres**: tek, düz bir ASCII adres (görünen ad yok, boşluk yok, en çok 254 karakter).
  İşletmenin bir yöneticisinin adresi **422** alır (`reason` `administrator_address`). İşletmenin
  başka bir **aktif** çalışanındaki adres **409 `email_unavailable`** alır: hiçbir şey
  oluşturulmaz, değiştirilmez ya da gönderilmez. Pasifleştirilmiş bir çalışanın adresi yeniden
  serbesttir.
- **Adresi değiştirmek**: eski adrese gönderilmiş her aktivasyon bağlantısı aynı değişiklikte
  çalışmaz hâle gelir. Aktive olmamış bir çalışana yeni bir aktivasyon e-postası gönderilir;
  **aktive olmuş bir çalışana gönderilmez**.
- **Yeniden işe alım**: çalışanı pasifleştirilmiş bir referansa `active: true` **yeni bir çalışan**
  oluşturur — aynı `externalRef` altında yeni bir `id`; önceki çalışan ve geçmişi olduğu gibi
  kalır. Kayıtlarınızı `id` ile anahtarlayın.
- **Adlar** lokasyon adı kuralına uyar ([bölüm 4](#locations)) ve bir adın içindeki sekme ya da
  satır sonu da `hidden_character`'dır.
- **Bu cevapların söyledikleri**: 409 `email_unavailable`, 422 `administrator_address` ve adresle
  bağlama, anahtarın sahibine bir adresin **kendi işletmesinin içinde** kime ait olduğunu söyler —
  işletmenin çalışanlarını yazan anahtardır. Başka hiçbir işletme hakkında bir şey söylemez.

```sh
curl -sS -X PUT "$TAPTIME_API/v1/employees/E-1042" \
  -H "Authorization: Bearer $TAPTIME_KEY" \
  -H "Content-Type: application/json" \
  --data-binary '{"firstName":"Jane","lastName":"Example","email":"jane.example@example.com","active":true,"employeeCode":"1042"}'
```

<!-- example: putEmployee 201 -->
```json
{
  "id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91",
  "externalRef": "E-1042",
  "employeeCode": "1042",
  "firstName": "Jane",
  "lastName": "Example",
  "fullName": "Jane Example",
  "active": true,
  "activated": false,
  "activatedAt": null,
  "hasEmail": true,
  "activationEmail": {"status": "queued", "at": "2026-10-16T09:13:12.905861Z"},
  "createdAt": "2026-10-16T09:13:12.891784Z",
  "updatedAt": "2026-10-16T09:13:12.902916Z"
}
```

API'nin hiçbir cevabı bir e-posta adresi taşımaz: `hasEmail` bir adres olup olmadığını söyler.

<a id="employees-activation"></a>
### 5.2 Aktivasyon

Adresi olan yeni bir çalışana **aktivasyon e-postası kendiliğinden** gider. E-posta bir kuyruktan
gönderilir: istek Taptime'ın gönderim sınırları yüzünden asla reddedilmez ya da yavaşlamaz — bunun
yerine e-posta gecikir — ve içindeki bağlantı e-posta gönderilirken oluşturulur.

Çalışan bağlantıyı telefonunda açar ve sonra **kendi şirketinin bir plaketine** (asla bir ortak
şirketinkine değil) **NFC ile** dokunur — QR kod aktive edemez. Bu ilk dokunuş aktivasyonu tamamlar
ve **bir punch değildir**: çalışanın ilk punch'ı bir sonraki dokunuşudur.

<a id="employees-email-state"></a>
### 5.3 Aktivasyon e-postasının durumu

`activationEmail.status` dört değerden biridir, `activationEmail.at` onun zamanıdır:

| Durum | Anlamı |
|---|---|
| `queued` | E-posta gönderilmeyi bekliyor — başarısız bir gönderim yeniden denenirken de. |
| `sent` | Bir e-posta teslim edildi ve bağlantısı hâlâ çalışıyor. |
| `undelivered` | Son deneme çalışana ulaşmadı ve kuyrukta bir şey yok: `resend-activation` gönderin. |
| `none` | Bunların hiçbiri — hiç istenmedi ya da bağlantı kullanıldı, süresi doldu ya da yenisiyle değişti. |

Kuyruktaki bir e-posta **en çok 72 saat** bekler — bir gönderim sınırının arkasında ya da Taptime
işletme için API'yi kapatmışken (API kapalıyken kuyruk durur, yeniden açılınca sürer; 72 saat her
iki durumda da işler). Gerçek bir gönderim hatası **5 kez** denenir. İkisinden biri dolunca durum
`undelivered`'dır; cevap hangisi olduğunu söylemez, çünkü çare aynıdır: `resend-activation`.

Bir işletme **saatte en çok 50, günde en çok 300 aktivasyon e-postası** gönderir ve bir çalışana
saatte en çok 3 e-posta gider. Tek seferde yaklaşık 900'den fazla çalışan oluşturan bir ilk
senkronizasyon bu yüzden her e-postayı 72 saat içinde gönderemez: kalanlar `undelivered` olur. Büyük
bir ilk senkronizasyonu birkaç güne yayın ya da `undelivered` biten çalışanlar için
`resend-activation` gönderin.

<a id="employees-resend"></a>
### 5.4 `resend-activation`

`POST /v1/employees/{ref}/resend-activation` aktivasyon e-postasının yeniden gönderilmesini ister.

- Her zaman çalışanla birlikte **202** cevaplar — hiçbir şeyi değiştirmediğinde de: bir e-posta
  kuyruktayken ya da son bir saatte bir e-posta gönderilmiş ve bağlantısı hâlâ çalışıyorken istek
  etkisizdir. İstendiği anda hiçbir bağlantıyı iptal etmez; yeni bağlantı, gönderildiğinde,
  eskilerinin yerini alır.
- Aktive olmuş bir çalışan **409 `already_activated`** alır: yeni bir telefon Taptime panelinden
  kurulur.
- Adresi olmayan bir çalışan 422 alır (`field` `email`, `reason` `required`).
- İşletme askıdayken kabul edilir.

<a id="employees-deactivate"></a>
### 5.5 Pasifleştirme

`DELETE /v1/employees/{ref}` ya da `active: false` ile `PUT` çalışanı pasifleştirir: o andan sonra
dokunuşları reddedilir, geçmişi kalır.

- **Pasifleştirme bütçesi**: bir anahtar saatte **20 çalışan** pasifleştirebilir. Bunun ötesinde
  cevap `Retry-After` ile **429 `deactivation_budget_exceeded`**'dir — saatin pasifleştirmelerinin
  en eskisi pencereden çıkana kadar kalan saniye — ve işletmenin sahibi panelde bir uyarı görür. Bu
  kod `rate_limited` değildir; sisteminiz bir bütçeyi bir istek sınırından ayırt edebilir.
- Zaten pasifleştirilmiş bir çalışan olduğu gibi cevaplanır: 200, bütçe harcanmaz — işletme
  askıdayken hariç.
- İşletmenin hiç tutmadığı bir referans **404 `not_found`** alır — işletme askıdayken bir
  `DELETE` hariç (aşağıda).
- İşletme askıdayken her `DELETE` 403 `tenant_suspended` alır — zaten pasifleştirilmiş bir çalışan
  ve işletmenin hiç tutmadığı bir referans için de —; var olan bir çalışana denk gelen,
  `active: false` ile her `PUT` de öyle, zaten pasifleştirilmiş bir çalışan dahil. İşletmenin hiç tutmadığı
  bir referansa `active: false` ile `PUT` hiçbir çalışana denk gelmez: 404 kalır
  ([bölüm 11](#suspension)).

<!-- example: deleteEmployee 200 -->
```json
{
  "id": "emp_32a024b8-e86f-4110-a4da-c05ee0acd9b7",
  "externalRef": "E-1043",
  "employeeCode": null,
  "firstName": "Sam",
  "lastName": "Placeholder",
  "fullName": "Sam Placeholder",
  "active": false,
  "activated": false,
  "activatedAt": null,
  "hasEmail": false,
  "activationEmail": {"status": "none", "at": null},
  "createdAt": "2026-10-16T09:13:12.918861Z",
  "updatedAt": "2026-10-16T09:13:12.95631Z"
}
```

<a id="employees-read"></a>
### 5.6 Çalışanları okumak

- `GET /v1/employees` işletmenin **bütün** çalışanlarını sayfalar — aktif ve pasifleştirilmiş,
  ayrıca Taptime panelinde oluşturulmuş ve henüz bir referansa bağlanmamış olanları da
  (`externalRef` `null`; `firstName` ve `lastName`'leri `null`'dır, `fullName` adlarıdır). Yalnız
  `limit` ve `cursor` alır. Dolu bir sayfa (`limit` çalışan) `nextCursor` taşır; onu `cursor` olarak
  gönderin. Daha kısa bir sayfa sonuncusudur ve `nextCursor`'ı `null`'dır.
- `GET /v1/employees/{ref}` referanstaki aktif çalışanı, aktif olan yoksa en son pasifleştirilmiş
  olanı cevaplar; işletme referansı hiç tutmadıysa 404.

<a id="feed"></a>
## 6. Punch akışı

`GET /v1/punches?scope=…` işletmenin punch'larını akışının onları aldığı sırayla okur ve hiçbirini
kaybetmez.

<a id="feed-what"></a>
### 6.1 Punch nedir

Punch, Taptime'ın **saydığı** bir dokunuştur:

- `ok` olarak karara bağlanmış bir dokunuş, NFC (`nfc`) ya da QR kodla (`qr`) — kaydedildiği anda
  akışa girer;
- `flag` olarak karara bağlanmış (kaydedilmiş, bir yöneticinin onayını bekleyen) bir dokunuş,
  **işverenin bir yöneticisi onu onaylayınca** — onay anında, bir kez akışa girer. Reddedilen bir
  flag asla girmez.

Başka hiçbir şey punch değildir ve başka hiçbir şey akışta yoktur:

- **aktivasyon dokunuşu** ([5.2](#employees-activation));
- bir yöneticinin Taptime panelinde girdiği **manuel kayıt** — manuel kayıtlar sizin sisteminizindir;
- **reddedilmiş** bir dokunuş (ör. pasifleştirilmiş bir çalışanınki ya da işletmenin kendisinin ya
  da bir ortak şirketin **kayıp ya da emekli bir plaketine** dokunuş) ve bir **pratik** dokunuşu;
- aynı kişinin **60 saniye** içindeki **tekrar** dokunuşu — telefonların beyan ettiği anlarda
  **ya da** Taptime'ın kendi saatinde, hangisi yakınsa: yok sayılır — plaket başına değil kişi
  başına, iki şirketin plaketleri arasında da. Bir istemci dokunuşları çevrimdışı bir kuyrukta
  tutup ikisini arka arkaya gönderirse ikisi Taptime'ın saatinde birbirine yakındır; beyan ettikleri
  anlar daha uzak olsa da ikincisi yok sayılır.

**Zaman.** `punchTime` **dokunuşun** anıdır: çalışanın telefonu bir an beyan ettiyse o an,
etmediyse Taptime'ın dokunuş için kendi anı. Taptime'ın dokunuş sayfası bugün hiçbir an beyan
etmez; yani bugün her `punchTime` Taptime'ın anıdır. `recordedAt` punch'ın akışa
girdiği andır (onaylanan bir flag için onayın anı). Bir an beyan eden istemci — örneğin
çevrimdışı yapılan bir dokunuşu bir kuyrukta tutup sonra, yapıldığı anla gönderen bir istemci
(bugün hiçbir Taptime istemcisi bunu yapmaz: dokunuş sayfası kuyruk tutmaz) — o anla karara
bağlanır: **120 saniyeye** kadar gecikmeli olan dokunuş (varsayılan; işletme bunu değiştirebilir)
canlı bir dokunuş gibi karara bağlanır; daha geç olanı bir yöneticinin onayını gerektirir; **72
saatten** daha eski ya da gelecekteki bir an reddedilir. Hemen gönderilen
bir dokunuş hiçbir an beyan etmez: `punchTime`'ı Taptime'ın dokunuş için kendi anıdır,
`recordedAt`'inden milisaniyeler uzaktadır — hikâyedeki gibi; hikâyenin dokunuşları canlıdır.

<a id="feed-scopes"></a>
### 6.2 Kapsamlar

`scope` **zorunludur**; şunlardan biri:

- `employer` — **bu işletmenin kendi çalışanlarının** punch'ları, kendi plaketlerinde ve bir ortak
  şirketin plaketlerinde (orada `location` ortağın lokasyonunu, `location.company` ortağın kodunu
  söyler);
- `location` — **bu işletmenin plaketlerindeki** her punch, bir ortak şirketin çalışanlarınınkiler
  dahil (orada `employee` ortağın çalışanını, `employee.company` ortağın kodunu söyler).

Bir işletmenin kendi çalışanının kendi plaketindeki punch'ı iki kapsamda da vardır. Bir grup
çalışanının ortak şirketin plaketindeki punch'ı işverenin `employer` kapsamında ve ortağın
`location` kapsamındadır — **ikisinde de aynı `id`** ile. Ortağa kim ve nerede bilgisi verilir:
çalışanın `id`, `externalRef`, `employeeCode` ve `company`'si, lokasyonun `id`, `externalRef`,
`name` ve `company`'si ve `tagId` — asla bir e-posta adresi, bir koordinat, bir ağ adresi, bir not ya
da bir kararın nedeni değil. Her punch gibi `direction`'ı ve onun `directionSource`'unu taşır:
işverenin kaydı, iki şirketin akışında da aynı.

Böyle bir punch'ın kararı **işverenindir**: onu işverenin kuralları karara bağlar ve ortağın kendi
lokasyonu için koyduğu bir kural işverenin oradaki çalışanına uygulanmaz — Taptime'ın koyduğu ve
hiçbir işletmenin değiştiremediği tek bir istisnayla: ortak şirketin lokasyonundaki bir QR punch'ı
ancak **o lokasyonun kendi ağından** geliyorsa sayılır; aksi hâlde işverenin onayını bekler.

İki şirket gruptan ayrılınca (Taptime üyeliği bitirince) aralarındaki punch'lar durur: plaket
sayacı üyelik bitmeden ilerletilmiş bir NFC dokunuşu yine kaydedilir; sonraki bir NFC dokunuşu
kayıtsız reddedilir (ve sayacı ilerletmez); aralarındaki bir QR punch'ı ve bir manuel kayıt bitiş
anından itibaren reddedilir.

<a id="feed-punch"></a>
### 6.3 Punch

| Alan | |
|---|---|
| `id` | `pch_…` — punch; iki kapsamda ve iki şirketin akışında aynı. |
| `version` | v1'de her zaman `1`. |
| `status` | v1'de her zaman `valid`: hiçbir punch iptal edilmez (`voided` hiç üretilmez). |
| `punchTime` | Dokunuşun anı (UTC): telefonun beyan ettiği an, beyan etmediyse Taptime'ın kendi anı — bugün her zaman Taptime'ınki ([6.1](#feed-what)). |
| `recordedAt` | Punch'ın akışa girdiği an (UTC); onaylanan bir flag için onayın anı. |
| `channel` | `nfc` ya da `qr`. |
| `direction` | `in` ya da `out`, **işverende** kaydedildiği gibi: Taptime'ın dokunuş anında hesapladığı yön ya da öbürü (`directionSource`). |
| `directionSource` | `direction` kimin: `auto` (Taptime'ın dokunuş anında hesapladığı yön) ya da `employee` (çalışanın; Taptime'ın hesapladığı yön değil — aşağıya bakın); akış bu alanı taşımadan önce kaydedilmiş bir punch için `null`. |
| `employee` | `id`, `externalRef`, `employeeCode`, `company` — punch anındaki hâlleriyle. |
| `location` | `id`, `externalRef`, `name`, `company` — punch anındaki hâlleriyle. |
| `tagId` | `tag_…`, dokunulan plaket — bir QR punch'ında da. |

- `employee` ve `location` alanları **punch anındaki** değerlerdir: sonradan yapılan bir yeniden
  adlandırma ya da yeni bir referans akıştaki bir punch'ı değiştirmez.
- Henüz bir referansa bağlanmamış bir çalışan `employee.externalRef: null` ile punch yapar;
  sisteminiz onu sonra `id` ile eşleyebilir (rehber ve `GET /v1/employees` onu cevaplar).
- `location` her zaman bir nesnedir. `id`, `externalRef` ve `name`'i bir lokasyona monte edilmemiş
  bir plaket için `null`'dır (`company` yine plaketin şirketini söyler); iki şirketin grup üyeliği
  bittikten sonra onaylanmış, ortağın lokasyonundaki bir punch için dördü de `null`'dır.
- `direction` işverende kaydedilen yöndür. Taptime onu takvim gününe göre değil, çalışanın
  işverendeki son açık girişine göre çıkarır (18:00 vardiyasının 02:00 çıkışı bir `out`'tur).
  `directionSource: auto` taşıyan bir punch için sisteminizin onu hesaplaması gerekmez;
  `directionSource: employee` taşıyan bir punch istisnadır (aşağıda).
- `directionSource: employee`, kaydedilen `direction`'ın Taptime'ın dokunuş anında hesapladığı yön
  olmadığı demektir. Çoğunlukla çalışan, Taptime'ın tahmin ettiği yönü gösteren dokunuş sayfasında
  öbürünü seçmiştir ("Not right?"); sayfa işverenin iş günü başlamadan önce açılıp başladıktan
  sonra basıldıysa sayfanın ana düğmesi de onu verebilir. `directionSource: auto`, kaydedilen yönün
  Taptime'ın hesapladığı yön olduğu demektir. Akış alanı taşımadan önce kaydedilmiş bir punch'ta
  `directionSource: null`'dır. Alan sürüm 1'e eklendi: onu okumayan bir istemci öbür her alanı
  eskisi gibi okur ([3](#common)).
- **`directionSource: employee` taşıyan bir punch'ı kendinden önceki punch'la eşlemeyin.** Böyle
  bir `out` kaydedildiğinde kapatacağı bir şey yoktur: ya çalışanın o iş gününde açık bir girişi yoktur ve
  Taptime `out`'u müdüre "Check-in missing" (giriş eksik) olarak listeler, ya da ondan önceki giriş
  18 saatten eskidir ve açık kalır. İki durumda da Taptime ona süre saymaz. Sonradan yazılan bir kayıt
  bunu değiştirebilir — çoğunlukla bir müdürün `out`'tan önceye girdiği manuel bir giriş; ondan sonra
  Taptime süreyi sayar — ve manuel bir kayıt akışta yoktur ([6.1](#feed-what)). Böyle bir `in` önceki
  girişi çıkışsız bırakır ve Taptime ona da süre saymaz. Saat için Taptime'ın raporu esastır — onu
  `GET /v1/timesheets` ile okuyun ([9](#timesheets)). Örnek,
  işverenin iş günü 05:00'te başlarken: dün 15:00'te bir `in`, sonra bugün 07:00'de "Not right? Clock
  out". Taptime 0 saat sayar ve 07:00 `out`'unu "Check-in missing" olarak listeler; ikisini eşleyen
  bir sistem 16 saat öder.
- API ile oluşturulan bir çalışanın Taptime'da ev lokasyonu yoktur; bu yüzden Taptime'ın kendi
  raporları onun punch'larını "başka bir lokasyonda" diye işaretlemez.

<a id="feed-paging"></a>
### 6.4 Akışı okumak

```text
cursor = load(scope)
repeat:
    page = GET /v1/punches?scope=<scope>&limit=1000[&cursor=<cursor>]
    for punch in page.data:
        upsert(punch.id, punch.version, punch)
    cursor = page.nextCursor
    save(scope, cursor)
    if page.hasMore is false:
        wait, then poll again with the same cursor
```

- `cursor`'sız başlayın (akışın başı). Her sayfanın `nextCursor`'ı bir sonraki isteğin imlecidir.
  **`nextCursor` her zaman doludur** — boş bir sayfada da; orada yetiştiğiniz konumdur — ve **asla
  geri gitmez**.
- Bir sayfa **`hasMore` `true` iken `limit`'ten az, hatta hiç punch taşımayabilir**: bir sayfa
  akışın en çok 10000 konumunu okur ve bir kapsam seyrek olabilir. `hasMore` true olduğu sürece
  okumaya devam edin. `hasMore: false`, sayfa okunduğu anda akışın tuttuğu her punch'ın sizde olduğu
  anlamına gelir; aynı `nextCursor` ile sonra yeniden yoklayın.
- **Kapsam başına bir imleç saklayın.** Bir imleç (`cur_…`) işletmenin, iki kapsamın paylaştığı
  akışında bir konumdur; öteki kapsama — ya da başka bir işletmeye — taşınan bir imleç o akışın
  aralığının içindeyse **reddedilmez** ve öncesindeki punch'lar **sessizce atlanır**. Asla
  taşımayın.
- Akışın sonunun ötesindeki bir imleç **400** alır. Sunucunun verdiği bir imleç için bu, yalnız
  Taptime'ın verisi sayaçları ileri alınmadan bir yedekten geri yüklendiğinde olur: durun ve
  Taptime'a bildirin — sessizce beklemeyin.
- `id` + `version` ile tekilleştirin: akış her punch'ı kapsam başına bir kez verir, ama bir webhook
  onu yeniden teslim edebilir ([bölüm 10.7](#webhook-duplicates)).

Hikâyede KF `location` kapsamını okur: bir KM çalışanı olan Joe Valletta'da giriş yaptı, Jane (KF)
de.

<!-- example: listPunches 200 feed -->
```json
{
  "data": [
    {
      "id": "pch_00d216d4-3012-4d76-9c35-8f24028b432a",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.008183Z",
      "recordedAt": "2026-10-16T09:13:13.051707Z",
      "channel": "nfc",
      "direction": "in",
      "directionSource": "auto",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    },
    {
      "id": "pch_04ce3ad0-fb32-477d-9d5d-7c0e9b079b41",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.083662Z",
      "recordedAt": "2026-10-16T09:13:13.116875Z",
      "channel": "nfc",
      "direction": "in",
      "directionSource": "auto",
      "employee": {"id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91", "externalRef": "E-1042", "employeeCode": "1042", "company": "KF"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    }
  ],
  "nextCursor": "cur_2",
  "hasMore": false
}
```

<a id="feed-working-time"></a>
### 6.5 Çalışma süresi ve `scope=location`

> **Dikkat — bir ortak şirketin çalışanının çalışma süresini `scope=location`'dan hesaplamayın.**
> Bir ortak şirketin çalışanının sizin lokasyonlarınızdaki punch'ları aradaki olayları — işverenin
> kendi plaketlerindeki punch'larını — içermez. Giriş ve çıkışı yalnız bu kapsamdan eşlemek çalışma
> süresini **fazla** sayabilir. Bu **`direction` ile de geçerlidir**: her punch'ın yönü doğrudur,
> ama eksik olan aradaki olaylardır — sizin lokasyonunuzdaki bir `in`'in eşi sizin lokasyonunuzda
> hiç görünmeyebilir ya da başka bir lokasyonun `out`'u olabilir. Bir grup çalışanının çalışma
> süresi için işveren şirketin verisi — onun `employer` kapsamı — esastır.

Hikâyede Joe (KM) vardiyasına KF'nin Valletta'sında başlar ve bir dakikadan biraz sonra KM'nin
Production plaketinde bitirir (hikâye gerçek zamanda koşar ve 60 saniye içindeki bir tekrar punch
olmazdı — [6.1](#feed-what)).
KF'nin `location` kapsamı yalnız onun `in`'ini alır (yukarıda). `out` bir KM lokasyonu punch'ıdır:
KM'nin `employer` ve `location` kapsamlarındadır. KM `employer` kapsamını bir geçmiş aralığı olarak
okur ve ikisini de görür:

<!-- example: listPunches 200 history -->
```json
{
  "data": [
    {
      "id": "pch_00d216d4-3012-4d76-9c35-8f24028b432a",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:13:13.008183Z",
      "recordedAt": "2026-10-16T09:13:13.051707Z",
      "channel": "nfc",
      "direction": "in",
      "directionSource": "auto",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_30258476-2ce5-4a5d-a7dd-36651c3019dd", "externalRef": "KF-VLT", "name": "Valletta", "company": "KF"},
      "tagId": "tag_0DD4F2FE3B9A6A"
    },
    {
      "id": "pch_f01406af-dfcc-4fc0-bc4e-c3d982645fe8",
      "version": 1,
      "status": "valid",
      "punchTime": "2026-10-16T09:14:14.253191Z",
      "recordedAt": "2026-10-16T09:14:14.261497Z",
      "channel": "nfc",
      "direction": "out",
      "directionSource": "auto",
      "employee": {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "externalRef": "M-2001", "employeeCode": "2001", "company": "KM"},
      "location": {"id": "loc_4b6d2cc5-44e7-42c0-9830-abd1b8e370bb", "externalRef": "DEP-1", "name": "Production", "company": "KM"},
      "tagId": "tag_DF83F315A119DF"
    }
  ],
  "nextCursor": "hst_2_1792142054253191_2_4b83bfee122c0b1d",
  "hasMore": false
}
```

Lokasyonun şirketinin (KF) Taptime paneli bir grup çalışanının kendi plaketlerindeki dokunuşlarını
listeler, ama onların çalışma süresini o da hesaplamaz: çalışma süresi işverenindir ve onu
işverenin çizelgesi sayar ([9](#timesheets)).

<a id="history"></a>
## 7. Geçmiş sorgusu

`GET /v1/punches?scope=…&from=…&to=…` kapsamın `punchTime`'ı [`from`, `to`) aralığındaki
punch'larını cevaplar.

- `from` ve `to` **birlikte** gider (biri ötekisiz 400 alır); `from`, `to`'dan önce olmalıdır ve
  aralarında en çok **31 gün ve bir saat** olabilir — her saat diliminde bir takvim ayı, yaz saati
  geçişi dahil. Başka her şey 400 alır (`details.maxHours` 745).
- Bunlar [bölüm 3.7](#query)'deki gibi RFC 3339 anlardır — saat farkının `+`'sını `%2B` olarak
  kodlayın.
- Punch'lar `punchTime` sırasıyla gelir; eşit anlar akış sırasıyla.
- Sayfalama akışınkiyle aynıdır: `nextCursor`, `hasMore`, `hasMore` true iken boş gelebilen
  sayfalar. Bir geçmiş imleci (`hst_…`) verildiği işletmeye, `scope`'a, `from`'a ve `to`'ya
  bağlıdır: başka biriyle 400 alır — akış imlecinin kapsamlar arası tuzağı burada yoktur. `from`/`to`
  ile bir akış imleci (`cur_…`) ya da onlarsız bir geçmiş imleci 400 alır.
- **Bir geçmiş okuması bir anlık görüntüdür.** İlk sayfası durduğu akış konumunu sabitler;
  sayfaları akışın o an tuttuğu punch'ları cevaplar. Akışa **sonradan, daha eski bir `punchTime`
  ile** giren bir punch — dokunuştan günler sonra onaylanan bir flag, bir istemcinin geç beyan
  ettiği bir dokunuş — o okumada **yoktur**, aralığının içinde olsa da; aynı aralığı sonra yeniden sormak **daha
  fazla** satır cevaplayabilir. Her punch'ı kesin almak için **akışı** okuyun ([bölüm 6](#feed));
  geçmiş sorgusunu geriye bakmak için kullanın.

<a id="directory"></a>
## 8. Rehber (directory)

`GET /v1/directory` işletmenin bilebileceği çalışanları cevaplar: **kendi çalışanlarını** ve bir
gruba aitse **ortak şirketlerin çalışanlarını**. Hiçbir gruba ait olmayan bir işletme yalnız
kendininkileri okur.

<a id="directory-entry"></a>
### 8.1 Kayıt

Her kayıt `id`, `company`, `externalRef`, `employeeCode`, `firstName`, `lastName`, `fullName`,
`active` (`false`: pasifleştirilmiş) ve `updatedAt` taşır — **e-posta adresi yok**. Taptime
panelinde oluşturulmuş ve bir entegrasyonun adlandırmadığı bir çalışanın `firstName` ve `lastName`'i
`null`'dır; `fullName` her zaman doludur.

<!-- example: listDirectory 200 -->
```json
{
  "data": [
    {"id": "emp_32a024b8-e86f-4110-a4da-c05ee0acd9b7", "company": "KF", "externalRef": "E-1043", "employeeCode": null, "firstName": "Sam", "lastName": "Placeholder", "fullName": "Sam Placeholder", "active": false, "updatedAt": "2026-10-16T09:13:12.95631Z"},
    {"id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91", "company": "KF", "externalRef": "E-1042", "employeeCode": "1042", "firstName": "Jane", "lastName": "Example", "fullName": "Jane Example", "active": true, "updatedAt": "2026-10-16T09:13:13.00516Z"},
    {"id": "emp_42f028c7-242a-4903-89df-8fde6176926b", "company": "KM", "externalRef": "M-2001", "employeeCode": "2001", "firstName": "Joe", "lastName": "Sample", "fullName": "Joe Sample", "active": true, "updatedAt": "2026-10-16T09:13:13.006277Z"}
  ],
  "nextCursor": null,
  "hasMore": false,
  "asOf": "2026-10-16T09:14:14.298977Z"
}
```

<a id="directory-walk"></a>
### 8.2 Yürüyüşler, `updatedSince` ve `asOf`

- Rehber **sayfalıdır** (`limit` en çok 1000). Kayıtlar (`updatedAt`, `id`) sırasıyla gelir.
- Bir **yürüyüş**, `cursor`'sız bir istekten `hasMore`'u `false` olan sayfaya kadarki sayfalardır.
  Sonraki sayfayı `cursor=nextCursor` ile ve **aynı `updatedSince`** ile (ya da hiç) isteyin: başka
  bir `updatedSince` ya da başka bir işletmenin imleci 400 alır. `nextCursor` yalnız `hasMore`
  `true` iken doludur, sonda `null`'dır.
- Bir yürüyüşün her sayfası yürüyüşün **`asOf`**'unu taşır (ilk sayfasının okunduğu an). Bir
  yürüyüş bitince sonrakine **`updatedSince=asOf`** ile başlayın. Sunucu `updatedSince`'ten **10
  dakika önceden** okur; geç commit edilmiş bir değişiklik kaçmaz.
- **Zaten sizde olan kayıtların gelmesini bekleyin** — bir yürüyüşün içinde de (siz okurken değişen
  bir çalışan sıranın sonuna gider): `id` ile üzerine yazın. Bir değişiklik, onu yapan yazma 10
  dakikadan uzun açık kalmadıkça sonraki bir yürüyüştedir.
- **`id` ile anahtarlayın**: yeniden işe alınan bir kişinin pasifleştirilmiş kaydı ile yeni aktif
  kaydı aynı `externalRef`'i taşıyabilir.

<a id="directory-full"></a>
### 8.3 Rehberin tamamını düzenli olarak okuyun

İki değişiklik **hiçbir** artımlı okumaya ulaşmaz, çünkü hiçbir çalışanı değiştirmez:

- bir şirketin **gruba girmesi ya da gruptan çıkması**: artımlı okuma yeni ortağın çalışanlarını
  getirmez, ayrılan ortağınkilerin gittiğini de söylemez;
- bir şirketin **`company` kodunun değişmesi**: artımlı okuma o şirketin çalışanlarını eski koduyla
  bırakır.

İkisinin de bir sinyali yoktur. **Rehberin tamamını** (`updatedSince`'siz bir yürüyüş) **düzenli
olarak — örneğin günlük —** ve haberdar olduğunuz bir grup değişikliğinden sonra okuyun; artımlı
okumalar aradaki değişiklikler içindir.

<a id="directory-clock"></a>
### 8.4 Sunucunun saati geri adım attıktan sonra reddedilen bir `nextCursor`

Nadiren, sunucunun kendisinin verdiği bir `nextCursor` 400 ile reddedilir — sunucunun saati geri
adım attıktan sonra. **Aynı isteği kısa ve giderek uzayan bir beklemeyle (saniyeler) yeniden
deneyin**; 400 sürerse yürüyüşü aynı `updatedSince` ile baştan alın. Hemen baştan almak işe yaramaz
(yeni yürüyüş aynı kayda varır); engel saatin geri adımı kadar sürer ve hiçbir şey kaybolmaz.
İşletme askıdayken rehber açıktır.

<a id="timesheets"></a>
## 9. Çalışma süresi çizelgesi

`GET /v1/timesheets?from=…&to=…` bir bordro dönemi için, işletmenin kendi her çalışanının Taptime'ın
**saydığı** çalışma süresini yerel gün gün cevaplar: Taptime'ın kendi raporunun, aynı motorla
hesaplanan rakamları. Bordronun okuması budur. Akış ([6](#feed)) değildir: müdürün girdiği manuel bir
kayıt punch değildir ([6.1](#feed-what)), ama süresi burada sayılır.

```sh
curl -sS "$TAPTIME_API/v1/timesheets?from=2026-11-01&to=2026-11-30" \
  -H "Authorization: Bearer $TAPTIME_KEY"
```

Sayılan, Taptime'ın raporunun saydığıdır:

- bir girişten bir çıkışa vardiya, iki ucu da `ok` — ya da işaretlenmiş ve bir müdür tarafından
  **onaylanmış**;
- girişini ya da çıkışını bir müdürün elle girdiği (bir **manuel kayıt**) vardiya — sayılır ve
  ayrıca `manualShifts`'tedir;
- bir müdürün onayını bekleyen vardiya (süresi `awaitingMinutes`'tadır), reddedilmiş olan ve
  practice dokunuşu **sayılmaz**;
- çıkışı olmayan bir giriş **sayılmaz**: Taptime çıkış tahmin etmez; giriş `openEntries`'tedir ve
  bir müdür çıkışı girene kadar süresi hiçbir rakamda yoktur. Girişinden 18 saatten sonra gelen bir
  çıkış da vardiya değildir: giriş açık kalır;
- girişi olmayan bir çıkış (`checkInMissing`) **sayılmaz**.

Giriş ile çıkış, dokunuşta olduğu gibi çalışanın son açık girişine göre eşlenir — takvim gününe göre
değil.

<a id="timesheets-period"></a>
### 9.1 Dönem

- `from` ve `to` işletmenin saat dilimindeki (`period.timeZone`) takvim günleridir, `YYYY-MM-DD`, ve
  **ikisi de dahildir**: `from=2026-11-01&to=2026-11-30` kasımdır.
- `to`, `from` ile aynı ya da ondan sonraki gündür ve bir dönem en çok **31 gündür**. Başka her şey —
  eksik bir tarih, `2026-11-31`, saatli bir tarih, daha uzun bir dönem — **400 `invalid_request`**
  alır; `details.parameters` `from` ve `to`'dur, `details.maxDays` 31'dir.
- **Bir vardiya başladığı yerel günde sayılır.** 18:00–02:00 gece vardiyası ilk günündür; dönemin
  son gününde başlayan vardiya bu dönemde tamamıyla sayılır (Taptime çıkışı için dönemin sonundan
  ötesini okur); `from`'dan önce başlayan vardiya, bu dönemin içinde bitse de önceki dönemindir.
  Birbirine değen iki dönem her vardiyayı bir kez sayar.
- Girişi olmayan bir çıkış kendi gününde `checkInMissing`'tedir. Sınırda bir istisna var: `from`
  günü işletmenin günü başlamadan önce yapılmış (iş günü bir önceki gün başlamış) ve girişi olmayan
  bir çıkış iki dönemin de rakamlarında yoktur; Taptime paneli onu "Check-in missing" olarak listeler.
- Dakikalar **tam dakikadır, aşağı yuvarlanır**; her rakam kendi kesin süresinden. Bir kişinin
  günleri `workedMinutes`'ından gün başına bir dakikadan az eksik toplanabilir; ödenecek rakam
  `workedMinutes`'tır.

<a id="timesheets-answer"></a>
### 9.2 Cevap

| Alan | |
|---|---|
| `period` | İstendiği gibi `from` ve `to`, ve `timeZone`: günlerin sayıldığı, işletmenin saat dilimi. |
| `complete` | Bkz. [9.3](#timesheets-paging). |
| `truncated` | Tek bir çalışanın kayıtları tek başına bir isteğin okuduğundan fazlaysa `true`: o çalışanın rakamları bir toplam değil bir tabandır ve `complete` `false`'tur. |
| `employee` | `id` (`emp_…`), `externalRef`, `employeeCode`, `firstName`, `lastName` ve `fullName` — rehberin kimlikleri ([8.1](#directory-entry)). |
| `workedMinutes` | Dönemin sayılan süresi. |
| `awaitingMinutes` | Bir müdürün onayını bekleyen süre — `workedMinutes`'ta değildir. |
| `shifts` | `workedMinutes`'ın arkasındaki vardiyalar. |
| `openEntries` | Çıkışı olmayan girişler. |
| `checkInMissing` | Girişi olmayan çıkışlar. |
| `manualShifts` | Girişini ya da çıkışını bir müdürün elle girdiği vardiyalar (`shifts`'ten). |
| `days` | Dönemin **her** günü, sırayla: `date`, `workedMinutes` (o gün başlayan vardiyaların), `manual` (biri elle girilmiş), `open` (o günün bir girişinin çıkışı yok) ve `checkInMissing` (o günün bir çıkışının girişi yok). |

- Yalnız işletmenin **kendi** çalışanları listelenir — dönemde kayıtları varsa pasifleştirilmiş biri
  de. Lokasyonlarınızda çalışmış bir grup ortağı çalışanı burada yoktur: o süre işverenindir ve
  işverenin çizelgesi onu sayar ([6.5](#feed-working-time)). Kendi çalışanınızın bir ortağın
  lokasyonundaki vardiyası sizindir ve buradadır.
- Dönemde kaydı olmayan çalışan listelenmez: sayılan süresi sıfırdır.
- Bir kaydın başka hiçbir şeyi cevapta yoktur: ağ adresi, koordinat, not ya da bir kararın gerekçesi
  yok, e-posta adresi de yok.

Hikâyede KM günün çizelgesini okur. Joe'nun vardiyası KF'nin Valletta'sında başladı ve bir dakikadan
biraz fazla sonra KM'nin Production'ında bitti: aşağı yuvarlanmış bir dakika — ve KM'nin süresi:

<!-- example: listTimesheets 200 km -->
```json
{
  "period": {
    "from": "2026-10-16",
    "to": "2026-10-16",
    "timeZone": "Europe/Malta"
  },
  "complete": true,
  "truncated": false,
  "data": [
    {
      "employee": {
        "id": "emp_42f028c7-242a-4903-89df-8fde6176926b",
        "externalRef": "M-2001",
        "employeeCode": "2001",
        "firstName": "Joe",
        "lastName": "Sample",
        "fullName": "Joe Sample"
      },
      "workedMinutes": 1,
      "awaitingMinutes": 0,
      "shifts": 1,
      "openEntries": 0,
      "checkInMissing": 0,
      "manualShifts": 0,
      "days": [
        {
          "date": "2026-10-16",
          "workedMinutes": 1,
          "manual": false,
          "open": false,
          "checkInMissing": false
        }
      ]
    }
  ],
  "nextCursor": null,
  "hasMore": false
}
```

KF aynı günü okur. Jane Valletta'da giriş yaptı ve çıkış yapmadı: girişi açıktır ve süre saymaz, bu
yüzden KF'nin sayfası tam değildir. Joe orada yoktur — süresi KM'nindir:

<!-- example: listTimesheets 200 kf -->
```json
{
  "period": {
    "from": "2026-10-16",
    "to": "2026-10-16",
    "timeZone": "Europe/Malta"
  },
  "complete": false,
  "truncated": false,
  "data": [
    {
      "employee": {
        "id": "emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91",
        "externalRef": "E-1042",
        "employeeCode": "1042",
        "firstName": "Jane",
        "lastName": "Example",
        "fullName": "Jane Example"
      },
      "workedMinutes": 0,
      "awaitingMinutes": 0,
      "shifts": 0,
      "openEntries": 1,
      "checkInMissing": 0,
      "manualShifts": 0,
      "days": [
        {
          "date": "2026-10-16",
          "workedMinutes": 0,
          "manual": false,
          "open": true,
          "checkInMissing": false
        }
      ]
    }
  ],
  "nextCursor": null,
  "hasMore": false
}
```

<a id="timesheets-paging"></a>
### 9.3 Sayfalar ve `complete`

- Çalışanlar `employee.id` sırasıyla gelir, sayfada en çok `limit` kadar (varsayılan 500, en çok 1000
  — [3.2](#paging)). `hasMore` `true` iken bir sayfa daha azını taşıyabilir: bir istek sınırlı sayıda
  kayıt okur ve sayfa, kayıtları sığmayacak bir çalışandan önce biter.
- `hasMore` `false` olana kadar `nextCursor`'u **aynı** `from` ve `to` ile `cursor` olarak gönderin;
  `nextCursor` tam olarak `hasMore` `true` iken vardır (sonda `null`). Başka bir dönemle ya da başka
  bir işletme tarafından gönderilen bir imleç **400 `invalid_request`** alır (`details.parameter`
  `cursor`).
- `complete`, **sayfadaki hiçbir çalışanın** onay bekleyen süresi, açık bir girişi ya da eksik bir
  girişi yoksa ve sayfa `truncated` değilse `true`'dur. Bir dönem, yürüyüşün **her** sayfası öyle
  dediğinde tamdır. Tam olmayan bir dönemi ödemeden önce işletmeden eksik olanı Taptime'da
  onaylamasını, düzeltmesini ya da girmesini isteyin.
- Cevap kayıtları **okuduğunuz andaki hâlleriyle** anlatır: bir müdürün düzeltmesi, bir onay ya da
  bitmemiş bir dönemde henüz gelecek bir dokunuş onu değiştirir. Ödemeden hemen önce dönemi yeniden
  okuyun.
- Yalnız `from`, `to`, `cursor` ve `limit` kabul edilir ([3.7](#query)). İşletme askıdayken bu uç
  açık kalır ([11](#suspension)).

<a id="webhooks"></a>
## 10. Webhook'lar

<a id="webhook-setup"></a>
### 10.1 Uç noktayı kurmak

Bir işletmenin **tek** bir webhook uç noktası vardır. Onu işletmenin **sahibi** Taptime panelinde
kurar (API keys bölümü, Webhook kısmı); API kuramaz. Adres şöyle olmalıdır:

- `https://` ve bir DNS adı — IP adresi değil;
- portsuz (ya da `:443` ile), kullanıcı adı ya da parola olmadan, sorgu dizgesi (`?`) ve parça (`#`)
  olmadan;
- uluslararasılaştırılmış bir ad (`xn--`) olmadan ve sonda nokta olmadan.

Panel adresi tek bir yazımla saklar (küçük harfli host, `:443` yok). Adres **Taptime bağlanırken**
yeniden denetlenir: özel, loopback, link-local ya da ayrılmış bir adrese, ya da Taptime'ın kendi
adresine çözülen bir ad reddedilir (`blocked_address`) — bir kez genel bir adrese çözülmüş olsa da.
Taptime **TLS 1.2 ya da daha yenisiyle** bağlanır ve sertifikayı denetler: sertifika uç noktanın adı
için geçerli olmalı ve herkesçe güvenilen bir sertifika otoritesine zincirlenmelidir — daha eski bir
TLS'le, kendinden imzalı ya da özel bir otoritenin sertifikasıyla her deneme başarısız olur.
Taptime **istemci sertifikası sunmaz**: bir tane isteyen alıcıda (karşılıklı TLS) da her deneme
başarısız olur — panel ve teslim günlüğü bunu başarısız bir güvenli bağlantı olarak adlandırır.
Yönlendirmeler **izlenmez**; cevap **10 saniye** içinde gelmelidir; cevabın gövdesi okunmaz.

<a id="webhook-key"></a>
### 10.2 İmza anahtarı

Taptime her isteği uç noktanın **imza anahtarıyla** imzalar: `tt_whsec_` ve ardından 43 karakter.
Panel onu **bir kez** gösterir — uç nokta oluşturulduğunda ve anahtar döndürüldüğünde — bir daha
asla. Bir döndürmeden sonra **eski anahtarla imzalanmış istek artık gelmez**: alıcınızı hemen yeni
anahtara geçirin (yeni imzayı reddeden bir alıcı teslimleri başarısız kılar ve teslimler
[bölüm 10.6](#webhook-retries)'nın takvimiyle yeniden denenir).

<a id="webhook-verify"></a>
### 10.3 İmzayı doğrulamak

Her istek `X-Taptime-Signature: sha256=<hex>` taşır: istek gövdesinin **ham baytları** üzerinde,
imza anahtarının **metniyle** (`tt_whsec_` öneki dahil) anahtarlanmış HMAC-SHA256'nın küçük harfli
onaltılık değeri. Zaman damgası başlığı yoktur.

```text
received = header("X-Taptime-Signature")
expected = "sha256=" + lower_hex(hmac_sha256(key = utf8(signing_key), message = raw_body))
if not constant_time_equal(received, expected):
    answer 401 and stop
event = parse_json(raw_body)
if event.event == "ping":
    answer 204 and stop
if already_stored(event.id, event.version):
    answer 204 and stop
store(event)
answer 204
```

HMAC'i ayrıştırmadan önce, baytlar **geldiği gibi** üzerinden hesaplayın: ayrıştırılıp yeniden
serileştirilmiş bir gövde aynı baytlar değildir. Sabit sürede karşılaştırın.

Uygulamanızı bu bilinen cevaplarla denetleyin. Anahtar bilerek sahtedir — `tt_whsec_` ve 43 tane
`A`'dır, asla gerçek bir anahtar değildir:

<!-- vector: webhook-signature -->
```text
key: tt_whsec_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
body: {"event":"ping"}
signature: sha256=1deefe0009103810c89395f2d078e1ba71edb003b3692a3ebca6d0866bf99935
body: (empty)
signature: sha256=88d05ff68b9d0e7585161401b08cb679fbf1701b3e28f683a3e96e4c33653576
body: {"event":"punch","deliveryId":"dlv_44721ce2-0f54-41ce-a757-7ee03cfec107","id":"pch_04ce3ad0-fb32-477d-9d5d-7c0e9b079b41","version":1,"status":"valid","punchTime":"2026-10-16T09:13:13.083662Z","recordedAt":"2026-10-16T09:13:13.116875Z","channel":"nfc","direction":"in","directionSource":"auto","employee":{"id":"emp_8973fbc8-5e76-48e2-a80b-67d5b10baa91","externalRef":"E-1042","employeeCode":"1042","company":"KF"},"location":{"id":"loc_30258476-2ce5-4a5d-a7dd-36651c3019dd","externalRef":"KF-VLT","name":"Valletta","company":"KF"},"tagId":"tag_0DD4F2FE3B9A6A"}
signature: sha256=c243d4f326b6a981d6dec95a7cca0360625eea97c8bd139045f943a76661339b
```

İlk bilinen cevap `openssl` ile — yukarıdaki ilk imzayı basar:

```sh
printf '%s' '{"event":"ping"}' | openssl dgst -sha256 -hmac 'tt_whsec_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' | sed 's/^.*= /sha256=/'
```

Bunu **yalnız sahte anahtarla** kullanın: `-hmac` (ve `-macopt`) anahtarı komut satırında alır, makinenin
başka kullanıcıları onu orada okuyabilir (`ps`). Gerçek anahtarla HMAC'ı alıcınızın kendi dilinde
hesaplayın — onun standart HMAC-SHA256'sıyla, anahtar sır deponuzdan okunarak — ve sabit zamanda
karşılaştırın.

<a id="webhook-requests"></a>
### 10.4 İstekler

Her istek üç başlıklı bir `POST`'tur — `Content-Type: application/json`,
`User-Agent: Taptime-Webhooks/1` ve `X-Taptime-Signature` — ve `event`'i ne olduğunu söyleyen bir
JSON gövdesi taşır.

**Bir punch** (`event` `punch`), iki alan fazlasıyla `GET /v1/punches` öğesidir
([bölüm 6.3](#feed-punch)): `event` ve `deliveryId` (`dlv_…`). İşletmenin akışındaki her punch bir
kez gönderilir (iki kapsamındaki bir punch tek bir teslimdir); tek bir gruptaki iki şirket
arasındaki bir punch **iki teslimdir**, her şirketin uç noktasına bir tane. Jane'in punch'ı, KF'nin
uç noktasının aldığı biçimiyle, yukarıdaki vektörlerin üçüncü gövdesidir.

**Bir test mesajı** (`event` `ping`) `{"event":"ping","id":"<uuid>","sentAt":"<RFC 3339 UTC>"}`
gövdesidir, aynı başlıklarla ve aynı imzayla. Sahip onu panelden gönderir, saatte en çok 10 tane —
"Send again now"ın harcadığı aynı 10. Hiçbir punch'ı adlandırmaz: **onu punch saymayın**.

<a id="webhook-answer"></a>
### 10.5 Cevap vermek

**Her 2xx "teslim edildi"dir.** Başka her şey başarısız bir denemedir: başka bir durum — **3xx
dahil** (yönlendirmeler izlenmez) —, 10 saniye içinde gelmeyen cevap, bir TLS ya da bağlantı hatası.
Hızlı cevap verin, sakladıktan sonra; işinizin geri kalanını sonra yapın.

<a id="webhook-retries"></a>
### 10.6 Yeniden denemeler

İlk deneme hemendir. Başarısız bir denemeden sonra bir sonraki 1 dakika, 5 dakika, 30 dakika, 2 saat,
6 saat bekler; **altıncı** başarısız deneme teslimi **başarısız** (`dead`) yapar ve kendiliğinden
bir daha denenmez. Her hata bu biçimde yeniden denenir — bir 4xx de (döndürülmüş anahtara henüz
geçmemiş bir alıcı, düzeltilmekte olan bir yol).

| Deneme | Önceki deneme başarısız olduktan sonra bekleme (saniye) |
|---|---|
| 1 | 0 |
| 2 | 60 |
| 3 | 300 |
| 4 | 1800 |
| 5 | 7200 |
| 6 | 21600 |

Sahip her teslimi panelin **Deliveries** listesinde görür — punch'ı, durumunu (Queued, Sending,
Retrying, Delivered, Failed), denemelerini ve son cevabın sınıfını ve durum kodunu (asla adresi ya
da cevabınızın metnini değil) — ve bekleyen ya da başarısız bir teslimi **Send again now** ile
yeniden gönderebilir (başarısız olan bir deneme daha alır). Hata sınıfları `timeout`, `tls`,
`refused`, `http_4xx`, `http_5xx` ve `blocked_address`'tir.

Liste bir teslimi ulaştıktan ya da başarısız sayıldıktan sonra **90 gün** tutar ve ondan sonra hiç
göstermez; teslim, işletmenizin teslimleri daha sonra koşarken silinir ve başarısız olan artık
yeniden gönderilemez. Hâlâ bekleyen bir teslim asla silinmez. Punch'ın kendisi akışta kalır.

<a id="webhook-duplicates"></a>
### 10.7 Tekrarlar ve sıra

- **Bir punch birden çok kez gelebilir**: bir yeniden deneme, Taptime yeniden başlarken uçuşta kalmış
  bir istek, bir "Send again now". Bir teslimin her denemesi aynı `deliveryId`'yi ve **aynı
  baytları** taşır. **`id` + `version` ile tekilleştirin.**
- **Sıra garantisi yoktur**: sonraki bir punch öncekinden önce gelebilir ve iki şirket arasındaki
  bir punch iki uç noktaya giden iki bağımsız teslimdir.

<a id="webhook-delays"></a>
### 10.8 Gecikmeler ve boşluklar

- **Taptime yeniden başladıktan sonra** (her deploy dahil) bekleyen yeniden denemeler — ve kuyruktaki
  aktivasyon e-postaları — işletmenin bir sonraki dokunuşuna, onayına ya da API isteğine kadar bekler.
  Akış etkilenmez.
- **Uç nokta kapatılmış**: zaten kuyrukta olan teslimler **bekler** (düşürülmez) ve sahip uç noktayı
  yeniden açınca (ya da adresini değiştirince, ya da anahtarını döndürünce) hemen gider — işletme
  askıdayken sahip bunların hiçbirini yapamaz ([bölüm 11](#suspension)). **Kapalıyken** kaydedilen
  punch'lar hiç teslim edilmez.
- **API Taptime tarafından kapatılmış**: aynısı — kuyruktaki teslimler bekler ve API yeniden
  açıldıktan sonra 15 dakika içinde gider (Taptime arada yeniden başladıysa işletmenin bir sonraki
  dokunuşunda, onayında ya da API isteğinde); kapalıyken kaydedilen punch'lar teslim almaz.
- Bir uç nokta, oluşturulduktan sonra kaydedilen punch'ları alır.

<a id="webhook-source"></a>
### 10.9 Kesin kaynak akıştır

Bir webhook hızlı bir haberdir, kayıt değildir: uç nokta kapalıyken kaydedilen punch'lar hiç teslim
edilmez ve `dead` bir teslim kendiliğinden geri gelmez. **Akışı** imleciyle ([bölüm 6.4](#feed-paging))
**düzenli olarak okuyun** ve webhook'u "anında haber" sayın; `id` + `version` ile tekilleştirmeyle
ikisi bir punch'ı asla iki kez saymaz.

<a id="suspension"></a>
## 11. Askıya alma

Taptime bir işletmeyi askıya alabilir (ör. faturalandırma yüzünden). Askı, okumaya — ve kayıtlara —
dokunmayan bir **yazma kilididir**:

| Askıda açık | Askıda 403 `tenant_suspended` |
|---|---|
| her `GET` — akış, geçmiş sorgusu, rehber, çalışanlar, çizelge | `PUT /v1/locations/{ref}` |
| **yeni** bir çalışan oluşturan `PUT /v1/employees/{ref}`, yeniden işe alım dahil | var olan bir çalışana denk gelen her `PUT /v1/employees/{ref}` — bir değişiklik, adresle bağlama, `active: false` (zaten pasifleştirilmiş bir çalışana da) ve hiçbir şeyi değiştirmeyen bir istek |
| `POST /v1/employees/{ref}/resend-activation` | her `DELETE /v1/employees/{ref}`, zaten pasifleştirilmiş bir çalışana ya da hiç tutulmamış bir referansa da |
| webhook teslimleri ve aktivasyon e-postalarının kuyruğu | panelde: anahtar oluşturmak; uç noktayı oluşturmak, adresini değiştirmek, anahtarını döndürmek ya da açmak; test mesajları; "Send again now" |
| dokunuşlar kaydedilir ve punch'lar akışa girer (bir grubunkiler de) | |
| panelde: bir anahtarı iptal etmek, uç noktayı kapatmak | |

Bir OAuth erişim tokenı ([bölüm 13](#oauth)) aynı tablonun altındadır. Yenilemesi ve iptali ile
sahibin Disconnect'i işletme askıdayken açık kalır; yeni bir bağlantı açık değildir.

<a id="differences"></a>
## 12. Sözleşme farkları

Taptime'ın, bir entegrasyonun varsayabileceğinden farkları. Her madde açıklandığı yere bağlanır.

1. Aktivasyon dokunuşu **punch değildir**: ilk punch, çalışanın ikinci dokunuşudur. Aktivasyon yalnız çalışanın **kendi şirketinin** bir plaketinde tamamlanır. ([5.2](#employees-activation))
2. `flag` olarak karara bağlanmış bir dokunuş akışa yalnız **işveren şirketin bir yöneticisi onaylarsa**, onay anında girer; reddedilen hiç gelmez. ([6.1](#feed-what))
3. v1'de `version` her zaman `1`'dir ve `voided` hiç üretilmez. ([6.3](#feed-punch))
4. Ek alanlar: `channel` (`nfc` | `qr`), `direction` (`in` | `out` — işverende kaydedilen yön) ve `directionSource` (`auto` | `employee` — onun Taptime'ın dokunuş anında hesapladığı yön olup olmadığı); `tagId` her iki kanalda doludur. ([6.3](#feed-punch))
5. `punchTime` dokunuşun anıdır — telefonun beyan ettiği an, beyan etmediyse Taptime'ın kendi anı (Taptime'ın dokunuş sayfası bugün hiçbir an beyan etmez); beyan edilen bir an varsayılan olarak 120 saniyeye kadar gecikmeyle `ok`'tur (işletme bunu değiştirebilir), tavan 72 saattir; `recordedAt` sunucunun kaydettiği andır. ([6.1](#feed-what))
6. 60 saniye içindeki tekrar dokunuş punch değildir — kişi bazlı, **iki şirketin plaketleri arasında da** — ve 60 saniye beyan edilen anlarda ya da Taptime'ın kendi saatinde, hangisi yakınsa, sayılır: bir istemci çevrimdışı bir kuyruktan iki dokunuşu arka arkaya gönderirse ikincisi, beyan edilen anları daha uzak olsa da, kaybolur. ([6.1](#feed-what))
7. Lokasyonun `timezone`'u işletmeninkine eşit olmalıdır, değilse cevap 422'dir. ([4](#locations))
8. Lokasyonda `active:false` yalnız bir etikettir. ([4](#locations))
9. Yeniden işe alım yeni bir `employee.id` üretir; `externalRef` aynı kalır. ([5.1](#employees-put))
10. API ile oluşturulan bir lokasyondaki dokunuşlar, ağ adres aralıkları ve koordinatı Taptime panelinde yazılana kadar onaya düşer. ([4](#locations))
11. E-posta adresi ASCII olmalıdır ve işletmenin bir yöneticisinin adresi olamaz (422); işletmenin başka bir aktif çalışanındaysa 409 `email_unavailable` alır. Aktivasyon e-postası her zaman kuyruktan gider. ([5.1](#employees-put))
12. `resend-activation`, bir e-posta kuyruktayken ya da son bir saatte gönderilmiş ve bağlantısı çalışıyorken etkisizdir (202). ([5.4](#employees-resend))
13. Aktive olmuş bir çalışana `resend-activation` 409 `already_activated` alır. ([5.4](#employees-resend))
14. Aktive olmuş bir çalışanın e-posta adresi değişirse aktivasyon e-postası gönderilmez. ([5.1](#employees-put))
15. Aktivasyon yalnız NFC iledir (QR kod aktive edemez). ([5.2](#employees-activation))
16. Webhook'larda sıra garantisi yoktur; `id` + `version` ile tekilleştirin. ([10.7](#webhook-duplicates))
17. Manuel kayıtlar akışa girmez. ([6.1](#feed-what))
18. Geçmiş sorgusu (`from`/`to`) en çok 31 gün ve bir saati kapsar (bir yerel takvim ayı, yaz saati geçişi dahil), sayfalıdır ve bir anlık görüntüdür — geç gelen punch'lar yalnız akışta kesin görünür. ([7](#history))
19. `updatedSince` (rehber) 10 dakikalık bir örtüşmeyle okur; tekrar gelen kayıtlar `id` ile ezilir. ([8.2](#directory-walk))
20. Taptime yeniden başladığında (her deploy dahil), bekleyen webhook yeniden denemeleri ve kuyruktaki aktivasyon e-postaları işletmenin bir sonraki dokunuşuna ya da API isteğine kadar bekler. Akış etkilenmez. ([10.8](#webhook-delays))
21. Henüz bir referansa bağlanmamış bir çalışanın punch'ı `employee.externalRef: null` ile gelir. ([6.3](#feed-punch))
22. Pasifleştirme anahtar başına saatlik bir bütçeyle sınırlıdır; aşılınca cevap 429'dur. ([5.5](#employees-deactivate))
23. API ve webhook, Taptime'ın işletme için açtığı erişimle çalışır. ([2.1](#auth-enable))
24. Ev lokasyonu olmayan bir çalışanın punch'ına "farklı lokasyon" notu düşmez. ([6.3](#feed-punch))
25. Şirketler arası bir punch **işverenin** kurallarıyla karara bağlanır; ortağın kendi lokasyonu için koyduğu bir kural işverenin çalışanına uygulanmaz. **İstisna:** şirketler arası bir QR punch'ı ancak **o lokasyonun ağından** geliyorsa sayılır; aksi hâlde onaya düşer — işveren bunu kapatamaz. ([6.2](#feed-scopes))
26. Grup üyeliği bittiğinde şirketler arasındaki punch'lar durur: NFC'de, sayacı üyelik bitmeden ilerletilmiş bir dokunuş yine kaydedilir, sonraki bir NFC dokunuşu kayıtsız reddedilir (sayaç ilerlemez); QR ve manuel kayıtlar bitiş anından itibaren reddedilir. ([6.2](#feed-scopes))
27. Şirketler arası bir punch **öbür şirketin kimliklerini** taşır: çalışanın `id`, `externalRef`, `employeeCode` ve `company`'si, lokasyonun `id`, `externalRef`, `name` ve `company`'si ve `tagId`. Bir punch'ın alanları **punch anındaki** değerlerdir (sonradan yapılan bir yeniden adlandırma onu değiştirmez). ([6.2](#feed-scopes))
28. Başka bir şirketin kayıp ya da emekli bir plaketine dokunuş reddedilir; punch olmaz. ([6.1](#feed-what))
29. Rehber (`GET /v1/directory`) **sayfalıdır** (`cursor`, en çok 1000). ([8.2](#directory-walk))
30. Bir KF lokasyonunda başlayıp **bir KM plaketinde** biten bir KM vardiyasında KF (`scope=location`) yalnız **girişi** alır; çıkış bir KM lokasyonu punch'ıdır ve KM'nin iki kapsamında gelir. Her punch **işverende** kaydedilen `direction`'ı taşır (`in`/`out`); entegrasyonun onu hesaplaması gerekmez — yalnız `directionSource: employee` taşıyan bir punch kendinden önceki punch'la eşlenmez ([6.3](#feed-punch)). Lokasyonun şirketinin Taptime paneli bir grup çalışanının dokunuşlarını listeler ama sürelerini hesaplamaz ("süre işverende"). ([6.5](#feed-working-time))
31. **`scope=location` ile çalışma süresi hesaplayan entegrasyon dikkat:** bir ortak şirketin çalışanının sizin lokasyonlarınızdaki punch'ları işverenin aradaki olaylarını (öbür şirketin plaketlerindeki dokunuşları) içermez; giriş ve çıkışı yalnız bu kapsamdan eşlemek süreyi **fazla** sayabilir. **Bu `direction` ile de geçerlidir:** her punch'ın yönü doğrudur, ama eksik olan **aradaki olaylardır** — sizin lokasyonunuzdaki bir `in`'in eşi orada hiç görünmeyebilir ya da başka bir lokasyonun `out`'u olabilir. Bir grup çalışanının süresi için işveren şirketin verisi (onun `employer` kapsamı) esastır. ([6.5](#feed-working-time))

<a id="oauth"></a>
## 13. OAuth ile bağlanan uygulamalar

Bir anahtar ([bölüm 2.2](#auth-keys)), işletmenin kendi sistemi için kendi kimlik bilgisidir. **Birçok
işletmeye hizmet veren bir uygulama** — örneğin bir bordro servisi — onların her birine bunun yerine
OAuth 2.0 ile bağlanır: işletmenin sahibi uygulamayı Taptime panelinde onaylar ve uygulama, o tek
işletme adına, sahibin onayladığı kapsamların rotalarında iş yapan bir erişim tokenı alır. 3 ile 11
arasındaki bölümlerde yazılı her şey bir token için de bir anahtar için olduğu gibi geçerlidir; farklar
bu bölümde adıyla yazılıdır.

Uygulama iki adresle konuşur: `/v1` için API'nin host'u (`$TAPTIME_API`, [bölüm 1.3](#overview-host))
ve üç OAuth ucu `/oauth/authorize`, `/oauth/token` ve `/oauth/revoke` için **Taptime'ın kendi
adresi** — API'nin değil, panelin host'u. Örnekler bu adresi `$TAPTIME_OAUTH` diye yazar; OpenAPI
belgesinin `oauth` güvenlik şeması üç URL'yi adıyla verir.

<a id="oauth-register"></a>
### 13.1 Kayıt

Uygulamayı Taptime kaydeder — kendi kendine kayıt yoktur. Taptime'a uygulamanın adını, dönüş
adreslerini (her biri `https://`, sorgusuz, parçasız) ve gereken kapsamları ([13.2](#oauth-scopes))
bildirin. Şunları alırsınız:

- bir `client_id`: `app_` ve bir UUID. Sır değildir; yetkilendirme URL'sinde dolaşır.
- bir client secret: `tt_cs_live_` ve ardından 43 base64url karakter. **Bir kez** gösterilir; onu
  sunucunuzda tutun, başka hiçbir yerde değil. Taptime istek üzerine onu değiştirebilir: eski secret bir
  sonraki istekten itibaren reddedilir, önceden verilmiş tokenlar çalışmaya devam eder.

Bir dönüş adresi kayıtlı birine **bayt bayt** uymalıdır: sona `/` eklenmez, harf büyüklüğü, port
farklı olmaz, `www.` varyantı olmaz. Uygulama başka bir host'ta da hizmet veriyorsa o adresin de
kaydedilmesini isteyin.

<a id="oauth-scopes"></a>
### 13.2 Kapsamlar

| Kapsam | Açtığı |
|---|---|
| `timesheets:read` | `GET /v1/timesheets` |
| `employees:read` | `GET /v1/employees`, `GET /v1/employees/{ref}`, `GET /v1/directory` |
| `employees:write` | `PUT /v1/employees/{ref}`, `DELETE /v1/employees/{ref}`, `POST /v1/employees/{ref}/resend-activation` |
| `locations:write` | `PUT /v1/locations/{ref}` |
| `punches:read` | `GET /v1/punches` (akış ve geçmiş sorgusu) |

- **Hiçbir kapsam bir başkasını içermez**: `employees:write`, `GET /v1/employees`'u açmaz.
- `GET /v1/business` her kapsamdan bir tokena açıktır ([13.9](#oauth-api)).
- Tokenın kapsamlarının açmadığı bir rota **403 `insufficient_scope`**'tur: `details.scope` rotanın
  gerektirdiği kapsamdır ve başlık
  `WWW-Authenticate: Bearer realm="taptime", error="insufficient_scope", scope="<scope>"`'tur.
  Bir anahtar onu hiç almaz.
- OAuth kapsamı, punch akışının `scope` parametresi (`employer` ya da `location`,
  [bölüm 6.2](#feed-scopes)) değildir: `GET /v1/punches?scope=employer` OAuth kapsamı `punches:read`'i ister.

<a id="oauth-authorize"></a>
### 13.3 Yetkilendirme isteği

Sahibin tarayıcısını `$TAPTIME_OAUTH/oauth/authorize`'a şu sorgu parametreleriyle gönderin, her biri
en çok bir kez:

| Parametre | Değer |
|---|---|
| `response_type` | `code` |
| `client_id` | sizin `client_id`'niz |
| `redirect_uri` | **zorunlu**; kayıtlı adreslerinizden biri, bayt bayt |
| `scope` | **zorunlu**; boşlukla ayrık, uygulama için kaydedilmiş kapsamların bir alt kümesi |
| `state` | isteğe bağlı; en çok 1024 bayt yazdırılabilir ASCII; değişmeden geri gelir |
| `code_challenge` | `code_verifier`'ınızın base64url SHA-256'sı, 43 karakter |
| `code_challenge_method` | `S256` — zorunlu; `plain` reddedilir |

Taptime panelindeki onay sayfasına `303` ile yanıt verir ([13.4](#oauth-consent)); tarayıcı izler.

Tarayıcı Taptime'ın hata sayfasında kalır ve **hiçbir adrese hiçbir şey gönderilmez**: sorgu dizesi
ayrıştırılamıyorsa; `client_id` eksik, tekrarlanmış, biçimsiz, bilinmiyor ya da kapatılmışsa; ya da
`redirect_uri` eksik, tekrarlanmış ya da uygulama için kayıtlı değilse. Başka her sorun — başka bir
parametrenin eksik, tekrarlanmış ya da biçimsiz olması, uygulamanın isteyemeyeceği bir kapsam —
`redirect_uri`'nize `error` olarak (`invalid_request`, `unsupported_response_type` ya da `invalid_scope`), `iss`
ve `state`'iniz ile geri gönderilir — ama yalnız geçerli bir `state`: tekrarlanmış, çok uzun ya da
yazdırılabilir ASCII dışında olanı `invalid_request` olarak `state`'siz döner; sahip karar verirken
Taptime'ın tutamayacağı kadar büyük bir istek de öyle.

`redirect_uri`'nize gelen her yanıt `iss` taşır (RFC 9207): bir `code`'u kullanmadan önce onun
`$TAPTIME_OAUTH` olduğunu ve `state`'in sizin gönderdiğiniz olduğunu denetleyin. `code_verifier`'ı — ve
sonra refresh tokenını — yalnız sunucunuzda, kullanıcının oradaki oturumuna bağlı tutun; ikisini de
tarayıcıya vermeyin.

<a id="oauth-consent"></a>
### 13.4 Onay

- İşletmenin **yalnız aktif bir sahibi** onaylar. Sayfa uygulamayı, işletmeyi ve tarayıcının döneceği
  host'u adıyla yazar. Bir yönetici, askıdaki bir işletme ve entegrasyonlarını Taptime'ın kapattığı bir
  işletme "Allow" görmez, yalnız uygulamaya "Back to" düğmesini görür; o
  `error=access_denied` ile yanıt verir.
- Bağlantı **tek bir işletme** içindir — sahibin oturum açtığı işletme. Sayfa, kişinin sahibi olduğu ve
  askıda olmayan öteki işletmelerini listeler; onaylamadan önce birine geçebilir.
- Sahip uygulamanın istediği kapsamları onaylar, hepsini ya da hiçbirini ("Don't allow" —
  `error=access_denied`).
- Sayfa isteği **10 dakika** tutar: onayı bu süre içinde verin (aradaki bir oturum açma dahil), yoksa
  uygulamadan yeniden başlayın.
- Bir işletmenin **uygulama başına tek bağlantısı** vardır: yeni bir onay ve onun kod takası önceki
  bağlantının yerine geçer ve öncekinin bütün tokenları iptal edilir.

"Allow" ile tarayıcı `code`, `state` ve `iss` ile `redirect_uri`'ye gider.

<a id="oauth-token"></a>
### 13.5 Kod takası

`POST $TAPTIME_OAUTH/oauth/token`, form gövdesi (`application/x-www-form-urlencoded`), istemci
**HTTP Basic** ile kimliğini doğrular (`client_id` ve client secret):

```sh
curl -sS -X POST "$TAPTIME_OAUTH/oauth/token" \
  --user "$TAPTIME_CLIENT_ID:$TAPTIME_CLIENT_SECRET" \
  --data-urlencode grant_type=authorization_code \
  --data-urlencode "code=$CODE" \
  --data-urlencode "code_verifier=$CODE_VERIFIER" \
  --data-urlencode "redirect_uri=https://app.example.com/callback"
```

- `code_verifier`: `A`–`Z`, `a`–`z`, `0`–`9`, `-`, `.`, `_` ve `~` karakterlerinden 43 ile 128 karakter (RFC 7636).
- `redirect_uri` gönderilmeyebilir; gönderilirse yetkilendirme isteğindekiyle bayt bayt aynı olmalıdır.
- Gövdede de `client_id`, Basic'teki olduğunda kabul edilir; gövdede bir `client_secret`
  `invalid_request`'tir.
- Bir kod **60 saniye** yaşar ve **ilk** sunumunda harcanır, başarısız bir sunum dahil. Harcanmış bir
  kodu yeniden sunmak, onun kurduğu bağlantıyı iptal eder: bir kodu bir kez takas edin, yanıt kaybolursa
  yetkilendirme isteğinden yeniden başlayın.
- Takasta bir kod ayrıca, onaydan bu yana işletme askıya alınmışsa, Taptime entegrasyonlarını kapatmışsa
  ya da onaylayan kişi artık aktif sahibi değilse reddedilir (`invalid_grant`).

Yanıt (`200`, `Cache-Control: no-store`):

```json
{"access_token": "tt_at_live_…", "token_type": "Bearer", "expires_in": 3599,
 "refresh_token": "tt_rt_live_…", "scope": "employees:read employees:write timesheets:read"}
```

`scope` erişim tokenının kapsamlarıdır, boşlukla ayrık, Taptime'ın bir sırasıyla; `expires_in` erişim
tokenının kalan saniyeleridir (bir saat, eksi yanıtın sürdüğü zaman).

<a id="oauth-refresh"></a>
### 13.6 Yenileme

`POST $TAPTIME_OAUTH/oauth/token`, `grant_type=refresh_token` ve `refresh_token` ile, istemci yukarıdaki
gibi doğrulanır. `scope` isteğe bağlıdır: bağlantının kapsamlarının bir alt kümesi yalnız o alt kümenin
erişim tokenını verir (yeni refresh tokenı bağlantınınkini korur); boş ya da daha geniş bir `scope`
`invalid_scope`'tur.

- **Her yenileme yeni bir çift döndürür.** Gönderdiğiniz refresh tokenı tükenir: her zaman en yeni
  refresh tokenını saklayın. Önceki erişim tokenı süresi dolana dek çalışır.
- **30 saniye içinde bir yeniden deneme** — aynı kullanılmış refresh tokenı, ilk kullanımından sonraki
  30 saniye içinde yeniden — yeni bir çift döndürür ve bağlantının öteki bütün tokenlarını iptal eder:
  yanıtı kaybolan bir yenileme kaybolmuş olmaz.
- **Bundan sonra** aynı kullanılmış refresh tokenı **bağlantıyı iptal eder** (`invalid_grant`): sahibin
  uygulamayı yeniden onaylaması gerekir.
- Tek bir refresh tokenıyla aynı anda iki yenileme göndermeyin. Bir zaman aşımından sonra bağlantıyı
  kapatın, **en az 10 saniye** bekleyin (ucun bir istek için kendi sınırı) ve ilk istekten sonraki 30
  saniye içinde yeniden deneyin.
- **90 gün** kullanılmayan bir refresh tokenı ölür; sahibin uygulamayı yeniden onaylaması gerekir.
- Refresh tokenını yalnız sunucunuzda, bağlantıya bağlı tutun; tarayıcıya asla vermeyin.
- Yenileme, işletme askıdayken ve Taptime entegrasyonlarını kapatmışken de çalışır.

<a id="oauth-revoke"></a>
### 13.7 İptal

`POST $TAPTIME_OAUTH/oauth/revoke` (RFC 7009), istemci yukarıdaki gibi doğrulanır, form gövdesi
`token=…` (`token_type_hint` kabul edilir ve gerekmez: önek hangi token olduğunu söyler).

- Bir **refresh tokenı** bağlantıyı bitirir: onun her tokenını, bir sonraki istekten itibaren.
- Bir **erişim tokenı** yalnız o tokenı bitirir. Bağlantıyı koparmak için refresh tokenını iptal edin.
- Yanıt gövdesiz `200`'dür — bilinmeyen ya da zaten iptal edilmiş bir token için de, hiçbir şey
  yapılmayan başka bir uygulamanın tokenı için de.

<a id="oauth-lifetimes"></a>
### 13.8 Ömürler

| Değer | Önek | Ömür |
|---|---|---|
| yetki kodu | `tt_ac_live_` | 60 saniye, tek sunum |
| erişim tokenı | `tt_at_live_` | 1 saat |
| refresh tokenı | `tt_rt_live_` | her yenilemede değişir; 90 gün kullanılmazsa ölür |
| client secret | `tt_cs_live_` | Taptime değiştirene dek |
| bekleyen onay isteği | — | 10 dakika |

Bir iptal, bir bağlantı kopması ve Taptime'ın uygulamayı kapatması bir sonraki istekte geçerlidir:
hiçbir şey önbelleğe alınmaz. Bu değerlerin her birini log'lardan, URL'lerden ve `X-Request-Id`'den
uzak tutun ([bölüm 2.2](#auth-keys)).

<a id="oauth-api"></a>
### 13.9 API'yi tokenla çağırmak

Erişim tokenını bir anahtar gibi gönderin: `Authorization: Bearer <token>`, `$TAPTIME_API`'de.

- `GET /v1/business` tokenın adına iş yaptığı işletmeyi adıyla verir — bağlantıyı altında tutacağınız
  `id`. `companyCode`'u `null` olabilir: Taptime onu işletmenin API'sini açarken yazar ve bir işletme
  bir uygulamaya bu olmadan da bağlanabilir ([2.1](#auth-enable)).
- Süresi dolmuş ya da iptal edilmiş bir token, kopmuş bir bağlantı ya da Taptime'ın kapattığı bir
  uygulama: **401 `unauthorized`**, `WWW-Authenticate: Bearer realm="taptime", error="invalid_token"`
  ile. Yenileyin — yenileme `invalid_grant` ise sahibin uygulamayı yeniden onaylaması gerekir.
- Entegrasyonlarını Taptime'ın kapattığı bir işletme: **403 `api_disabled`**. Bağlantı kalır: yenileme
  yine çalışır ve Taptime onları yeniden açınca aynı tokenlar çalışır.
- **Bir holding ortağının verisi tokenla hiç gelmez** ([1.2](#overview-companies)): rehber yalnız
  işletmenin kendi çalışanlarını listeler, akış ve geçmiş sorgusu yalnız işletmenin kendi punch'larını
  taşır — grubun şirketleri arasındaki bir punch iki kapsamda da dışarıda kalır. Çizelge zaten
  işletmenin kendi çalışanlarınındır. Bir anahtarla yanıtlar 6 ile 8 arasındaki bölümlerin dediği gibidir.
- Oran sınırları ([3.4](#rate-limits)): bir token istemci adresinin bütçesine bir anahtar gibi sayılır ve
  kabul edildiğinde geri verilir — kabul edilen bir token bunun yerine bağlantı başına (dakikada 600) ve
  bütün bağlantılarında uygulama başına (dakikada 6000) sayılır. Reddedilen tokenlar adresin bütçesini
  harcar.
- Bir yazmanın pasifleştirme bütçesi ve Idempotency-Key'leri, bir anahtarınkiler onun kendisinin olduğu
  gibi, bağlantının kendisinindir. `employees:write` bir tokenın istediği aktivasyon e-postası, Taptime
  işletmenin API'sini açmamış olsa da gönderilir.
- Bir askı ([bölüm 11](#suspension)) bir tokenın yazmalarını bir anahtarınkileri kapattığı gibi kapatır.

Sandbox'ta ([1.4](#overview-sandbox)) değerler `_live_` yerine `_test_` ile başlar (`tt_at_test_` vb.)
ve orada kaydedilen bir uygulama bir loopback dönüş adresi de kullanabilir — `http://`, ardından
loopback adı ya da IPv4 loopback adresi (IPv6 olanı değil), ardından bir port — yine bayt bayt eşleşir.
Prod her loopback dönüş adresini reddeder. POST /v1/sandbox/taps bir tokena açık değildir (403
`insufficient_scope`): bir dokunuşu işletmenin `tt_test_` anahtarıyla simüle edin.

<a id="oauth-disconnect"></a>
### 13.10 Bağlantıyı koparmak

İşletmenin sahibi bağlantıyı Taptime panelinin **Developers** sayfasında, entegrasyon anahtarlarının
altındaki **Connected apps** kartında görür: uygulamayı, kapsamlarını, ne zaman ve kimin bağladığını ve en son ne zaman kullanıldığını. İşletmenin
herhangi bir sahibi onu **Disconnect** edebilir — tokenlarıyla bir sonraki istek 401'dir. Bağlantı
onaylayan kişinin değil, işletmenindir: o kişi ayrıldığında da çalışmaya devam eder. Taptime bir
uygulamayı bütün işletmeler için kapatabilir: tokenları 401'dir ve token ucu `invalid_client` yanıtını verir.

<a id="oauth-errors"></a>
### 13.11 OAuth uçlarının hataları

`/oauth/token` ve `/oauth/revoke`, `/v1`'inkini **değil**, RFC 6749'un hata biçimini döndürür: bir JSON
gövde `{"error": "<code>", "error_description": "<a fixed sentence>"}`. `error`'a göre davranın; açıklama
bir kişi için değil, log'larınız içindir. İki ucun kendi bütçesi vardır: istemci adresi başına dakikada 60
istek (istemcisi reddedilen bir istek yerini korur) ve kimlik bilgileri kabul edildiğinde uygulama
başına dakikada 600.

| `error` | Durum | Ne zaman |
|---|---|---|
| `invalid_request` | 400 | eksik, tekrarlanmış ya da biçimsiz bir parametre; Basic ile gövdedeki bir kimlik bilgisi uyuşmuyor |
| `invalid_client` | 401 | istemci bilinmiyor, kapatılmış ya da secret yanlış (`WWW-Authenticate: Basic realm="taptime"`) |
| `invalid_grant` | 400 | kod ya da refresh tokenı bilinmiyor, süresi dolmuş, harcanmış, iptal edilmiş ya da başka bir uygulamanın; `code_verifier` ya da `redirect_uri` uyuşmuyor; kod takasında: işletme askıda, Taptime entegrasyonlarını kapatmış ya da onaylayan artık aktif sahibi değil |
| `unsupported_grant_type` | 400 | `authorization_code` ve `refresh_token` dışında bir `grant_type` |
| `invalid_scope` | 400 | bir yenilemenin `scope`'u boş, bilinmeyen ya da bağlantınınkinden geniş |
| `temporarily_unavailable` | 429 | çok fazla istek — `Retry-After` ile |
| `temporarily_unavailable` | 503 | Taptime tarafında bir arıza; sonra yeniden deneyin |

`temporarily_unavailable` bu uç için RFC 6749'un kümesinde yoktur: HTTP durumuna da bakın.

<a id="changes"></a>
## 14. Bu rehberdeki değişiklikler

| Baskı | Tarih | Değişiklikler |
|---|---|---|
| 1 | 2026-10-16 | API sürüm 1 için ilk baskı. |
| 2 | 2026-10-18 | §1.4 sandbox'ı anlatır: `tt_test_` anahtarlar, kendi operatörü, e-posta yok ve dokunuş simülasyonu (alan adı henüz seçilmedi). |
| 3 | 2026-10-22 | §6.3: bir punch `directionSource`'u (`auto` \| `employee` \| `null`) taşır — `direction`'ı kimin: Taptime'ın dokunuş anında hesapladığı yön mü, değil mi. Örnekler ve imzanın bilinen cevabı onu taşır. §3: sürüm 1 içinde yanıtlara ve webhook gövdelerine alan eklenebilir — tanımadığınızı yok sayın. 1. ve 2. baskının yanıt şemaları kapalıydı (`additionalProperties: false`); bu baskı bütün yanıt ve webhook şemalarını açar; istek gövdeleri kapalı kalır. |
| 4 | 2026-11-14 | §9 yeni: `GET /v1/timesheets`, işletmenin kendi çalışanlarının bir bordro dönemindeki sayılan çalışma süresi, yerel gün gün — Taptime'ın kendi raporunun rakamları. 9 ile 12 arasındaki bölümler artık 10 ile 13 arasıdır. §3.1: `details.maxDays`; §3.5: `timesheetMaxDays`. |
| 5 | 2026-11-15 | §13 yeni: OAuth ile bağlanan uygulamalar — kayıt, yetkilendirme isteği ve onay, kod takası, yenileme, iptal, kapsamlar, `GET /v1/business` ve tokenla `/v1`'de değişenler; "Bu rehberdeki değişiklikler" artık §14'tür. §2.2: `X-Request-Id` OAuth öneklerini de reddeder. §3.4, §3.5: `requestsPerGrantPerMinute`, `requestsPerAppPerMinute`. §1.5: API'de OAuth vardır. |
