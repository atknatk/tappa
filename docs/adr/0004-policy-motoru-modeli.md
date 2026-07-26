# ADR 0004 — Policy motoru modeli

- **Durum:** kabul edildi
- **Tarih:** 2026-07-26

## Bağlam

Tappa'nın kararları bugün [CLAUDE.md](../../CLAUDE.md) §5'te **kodda gömülü bir
`if` zinciri** olarak tarif ediliyor: etiket durumu → SUN → oturum → çalışan
durumu → debounce → kanıt. Bu zincir doğru ama iki sorunu var. Birincisi
**müşteriye kapalı**: "statik IP'si olmayan şubemde GPS yeterli olsun" veya "QR'da
IP zorunlu kalsın ama şu şubede gevşesin" gibi kararlar müşteriden müşteriye
değişir ([open-questions.md](../plan/open-questions.md) Q15–Q17, Q21), ama koda
gömülü bir zincir bunları ifade edemez. İkincisi **kırmızı çizgi ile tercihi
karıştırır**: aynı `if` bloğunda hem §4 (biyometri yasağı, replay koruması, tenant
izolasyonu) hem de "GPS yeterli mi" tercihi durur; birini gevşetmek istediğinde
diğerini de yanlışlıkla açma riski vardır.

Bu ADR, [m3-policy-motoru.md](../plan/m3-policy-motoru.md)'de zaten **normatif
olarak tasarlanmış** policy motoru modelini gerekçeleriyle karara bağlar. Yeni bir
tasarım icat etmez; kart + spec ne diyorsa onu ADR biçiminde, "ne" değil **"neden"**
merkezli sabitler. Motor **genel amaçlıdır**, yalnız tap'e ait değil: aynı motor
"FLAGGED onayını kim verebilir", "manuel kayıt kim girebilir", "rapor kim dışa
aktarabilir" sorularına da bakar — bu yüzden eylemler ad alanlıdır (`tap:record`,
`policy:edit`, `report:export`, …).

v1 kapsamı bilinçli olarak dardır (Q22): mimari tam kurulur (belge modeli,
sürümleme, üç katman, değerlendirici, açıklanabilirlik) ama tenant'ın serbest JSON
yazması ve simülatör pilot sonrasına ertelenir. Tenant v1'de baseline
politikalarını **form üzerinden** açar/kapatır ve parametrelerini ayarlar. Bu ADR
bu kapsamdan bağımsızdır: model tam olarak yazılır, çünkü `policy_context` anlık
görüntüsü ([M3-07](../plan/m3-policy-motoru.md)) **1. günden** yazılmalı ki geçmiş
sonradan yeniden değerlendirilebilsin.

Bu ADR yazılmadan [M3-02](../plan/m3-policy-motoru.md)…M3-07 başlamamalıdır.
Sıfır hafızalı bir ajan M3-03 (ayrıştırma/doğrulama) ve M3-04 (değerlendirici) için
gereken her kararı **tek yorumla** bu belgeden çıkarabilmelidir; bu ADR'nin kabul
kriteri budur.

## Karar

### 1. Belge yapısı: AWS IAM'den alınan `Statement` modeli

Politikalar, AWS IAM'in belge yapısını **birebir** ödünç alan JSON belgeleridir:
`Statement[]` · `Effect` · `Action` · `Resource` · `Condition`. Her ifade (`sid`
ile adlandırılmış) bir `effect`, bir veya daha çok `action`, bir veya daha çok
`resource` ve bir `condition` taşır. Belge biçimi:

```json
{
  "version": "2026-07-24",
  "name": "GPS-only taps need review",
  "statements": [
    {
      "sid": "gps-only-review",
      "effect": "review",
      "action": ["tap:record"],
      "resource": ["location/*"],
      "condition": { "Bool": { "tap:ipMatch": false, "tap:gpsMatch": true } },
      "reason": "verified via GPS only — no network proof of place"
    }
  ]
}
```

**Neden IAM modeli.** Bilinen, denenmiş ve okunabilir bir yapıdır: kaynak/eylem/
koşul ayrımı, "kim neyi hangi bağlamda yapabilir" sorusunun endüstri-standart
cevabıdır. Sıfırdan bir DSL icat etmek hem bakım yükü hem de her yeni tenant kuralı
için yeniden öğrenme maliyeti getirirdi. IAM'in belge şekli aynı zamanda
**versiyonlanabilir bir veri**dir (kod değil) — bu, madde 9'daki append-only
sürümlemeyi ve [M3-07](../plan/m3-policy-motoru.md)'deki karar-kaydı bağını doğal
kılar.

### 2. Beş effect: `allow | review | deny | ignore | redirect`

IAM iki effect kullanır (`Allow` | `Deny`); Tappa'nın **beş** olası sonucu vardır,
çünkü bir tap'in sonucu iki değil beştir. Effect'ler ürünün karar sonuçlarıyla
birebir eşleşir:

| Effect | §5 / ürün sonucu | Anlamı |
|---|---|---|
| `allow` | `ok` | kabul, saate sayılır |
| `review` | `flag` | kayıt yazılır, onay kuyruğuna düşer (§4.6) |
| `deny` | `reject` | reddedilir (kayıt yazılabilir — §4.6, invariant) |
| `ignore` | `ignored` | debounce; kayıt yazılmaz, yön zincirine girmez |
| `redirect` | aktivasyon sayfası | kayıt **yok**, kullanıcı yönlendirilir |

**Neden beş, dört değil.** `ignore` (debounce) ve `redirect` (oturumsuz aktivasyon
/ tenant uyuşmazlığı), `deny`'den anlamca **farklıdır**: `deny` bir reddir ve §4.6
gereği çoğu durumda kayıt yazar; `ignore` meşru bir yinelemeyi yutar (kayıt
yazmamak *doğru* davranıştır); `redirect` henüz bir tap bile olmamıştır. Bu üçünü
tek `deny` altında toplamak, "kayıt asla kaybolmaz" (§4.6) ile "aynı kişi 60 sn'de
tekrar → kayıt yazma" arasındaki farkı silerdi.

### 3. İki farklı varsayılan: `tap:*` → `review`, yetkilendirme → `deny`

Eşleşen hiçbir politika yoksa sonuç **eylem ad alanına göre** iki farklı
varsayılana düşer:

- **`tap:*` eylemleri → `review`** (fail-to-review). §4.6 gereği kanıt yetersizse
  kayıt atılmaz, `flag`'lenir ve onay kuyruğuna düşer. Sessiz onay imkânsız, sessiz
  ret de imkânsız.
- **Yetkilendirme eylemleri → `deny`** (fail-closed): `policy:edit`,
  `report:export`, `tap:approve`, `record:manual`, `record:review`,
  `employee:deactivate`.

**Neden iki varsayılan, tek değil.** Tek bir "eşleşme yoksa `review`" varsayılanı
tap için doğru ama yetkilendirme için **felakettir**. Yeni bir tenant
provisioning'inde ([M7-03](../plan/m7-portal.md)) henüz hiçbir yetki politikası
bağlanmamışken "eşleşme yok → review" kuralı, `report:export` veya `policy:edit`
için **fail-open** anlamına gelirdi: pratikte *herkes* raporu dışa aktarabilir ve
politika düzenleyebilir olurdu (K2). "review" bir yetki eylemi için zaten
anlamsızdır — bir raporu "onay kuyruğuna almak" diye bir şey yoktur. Bu yüzden
yetkilendirme tarafı fail-closed olmak **zorundadır**: hiç kural bağlanmamış bir
sistemde en güvenli varsayılan "hiç kimse yapamaz"dır.

### 4. Üç katman ve §5 ile birebir eşleşme

```
1. guardrail   sistem, DEĞİŞTİRİLEMEZ, kapatılamaz.  §4 + §5 satır 1–5.
               Eşleşirse TERMİNAL — alt katmanlar hiç çalışmaz.
2. baseline    Tappa'nın yönetilen politikası, her tenant'a varsayılan bağlı.
               §5 satır 6–7. Tenant devre dışı bırakabilir veya kendi
               sürümüyle değiştirebilir.
3. tenant      Müşterinin yazdığı politikalar.
```

Değerlendirme akışı: **guardrail** sıralı çalışır, ilk eşleşen terminaldir ve alt
katmanları hiç çalıştırmaz → guardrail eşleşmezse **baseline + tenant birlikte**
değerlendirilir, en kısıtlayıcı effect kazanır (`deny` > `ignore` > `review` >
`allow`) → hiç eşleşme yoksa madde 3'teki varsayılan.

**Guardrail hiçbir koşulda gevşetilemez.** Guardrail'ler kodda gömülüdür,
`sys:` ad alanındadır ve **DB'de tutulmaz** ([M3-02](../plan/m3-policy-motoru.md));
DB'ye yazılabilseydi tek bir SQL erişimi kırmızı çizgileri kapatabilirdi. Devre
dışı bırakma API'si yoktur. Bir tenant belgesi ne kadar geniş bir `allow` yazarsa
yazsın, guardrail'in `deny`/`ignore`/`redirect` kararını `allow`'a **çeviremez** —
guardrail terminaldir ve baseline/tenant katmanı guardrail eşleştiğinde hiç
çalışmaz. Bu, IAM'deki SCP / permission boundary'nin karşılığıdır, ama gevşetilemez
biçimidir.

**CLAUDE.md §5 ↔ katman eşlemesi (normatif).** Bu ADR §5 karar tablosunun her
satırını bir katmana ve bir guardrail/baseline `sid`'ine bağlar:

| §5 satır | Koşul | §5 sonucu | Katman | Effect | sid ([M3-05](../plan/m3-policy-motoru.md)/[M3-06](../plan/m3-policy-motoru.md)) |
|---|---|---|---|---|---|
| **1** | etiket `lost`/`retired` | `reject` | **guardrail** | `deny` | `sys:tag-not-active` |
| **2** | SUN geçersiz (CMAC veya `ctr`) | `reject` | **guardrail** | `deny` | `sys:sun-invalid` |
| **3** | oturum yok/geçersiz | aktivasyon (kayıt yok) | **guardrail** | `redirect` | `sys:no-session` |
| **4** | çalışan `deactivated` | `reject` + uyarı | **guardrail** | `deny` | `sys:employee-deactivated` |
| **5** | aynı **kişi** 60 sn'de tekrar | `ignored` | **guardrail** | `ignore` | `sys:person-debounce` |
| **6** | IP eşleşti **veya** GPS < 150 m | `ok` | **baseline** | `allow` | `base:ip-or-gps-ok` · `base:gps-only-allow` |
| **7** | ikisi de yok | `flag` | **baseline** | `review` | `base:no-evidence-review` |

Yani **§5 satır 1–5 = guardrail** (`sys:*`, hiçbir müşteri kapatamaz) ve **§5 satır
6–7 = baseline** (`base:*`, tenant değiştirebilir) — CLAUDE.md §5'in kendi ifadesiyle
birebir. Guardrail katmanı **§5 satır 1–5'in üst kümesidir**: `sys:tenant-mismatch`
(sıra 1, ADR 0002 Y2/K3), `sys:tap-freshness` (sıra 4, [M5-10](../plan/m5-tap-akisi.md)),
`sys:occurred-at-bound` (sıra 5, K1), `sys:policy-edit-owner-only` (sıra 9, K2),
`sys:no-self-review` (sıra 10, Y-C) §5'in beş satırına ek olarak durur. Bunlar §5 ile
**çelişmez**: hepsi ya bir §4 kırmızı çizgisinin uygulanmasıdır (tenant izolasyonu =
§4.5; `occurred_at` zaman bütünlüğü = §4.3/§4.6'nın delil değeri) ya da motorun
kendisini koruyan yetkilendirme guardrail'idir (K2, Y-C). CLAUDE.md §5 guardrail'i
zaten "**§4 + §5 satır 1–5**" olarak tanımlar; bu üst küme o tanımın içindedir.

### 5. Guardrail sırası NORMATİFTİR

Guardrail'ler [M3-05](../plan/m3-policy-motoru.md)'teki **1→10 sırasıyla** çalışır
ve ilk eşleşen terminaldir. §5 "ilk eşleşen kazanır" der; bu, sıralamanın bir stil
tercihi değil **güvenlik sınırı** olduğu anlamına gelir. Sıra kodda tek bir yerde,
sıralı liste olarak tanımlanır ve testi ([M3-08](../plan/m3-policy-motoru.md))
1→10'u doğrular. Yanlış sıranın somut sömürüleri:

- **`sys:sun-invalid` (sıra 3) `sys:employee-deactivated` (sıra 7)'den önce
  gelmeli.** Tersi olsaydı: elinde eski bir URL ve çalınmış bir çerez olan biri,
  önce deaktivasyon kontrolünden geçerek **hesabın deaktive olup olmadığını
  öğrenirdi** (bilgi sızıntısı) — oysa SUN geçersizken kimliğe dair hiçbir şey
  sızmamalı. Üstelik deaktivasyon kararı bir güvenlik uyarısı ürettiği için, saldırgan
  geçersiz SUN'larla müdürün telefonuna **sınırsız push** yollayabilirdi (uyarı
  yorgunluğu → gerçek olay gürültüde kaybolur).
- **`sys:sun-invalid` (sıra 3) `sys:person-debounce` (sıra 8)'den önce gelmeli.**
  Tersi olsaydı debounce, geçersiz/replay bir SUN'ı "yakın zamanda gördük" diye
  yutup **replay penceresi** açardı; §4.4'ün kapattığı deliği yeniden açmak olurdu.

Bu iki hatayı `tappa-security-auditor` (R8) özellikle arar.

### 6. `ignore` ve `redirect` tenant'a kapalıdır

`ignore` ve `redirect` effect'lerini **yalnız guardrail** üretebilir; `baseline`
ve `tenant` belgelerinde bu iki effect **doğrulamada reddedilir**
([M3-03](../plan/m3-policy-motoru.md)).

**Neden.** Bir tenant `ignore` yazabilseydi, *"22:00'den sonraki her tap `ignore`"*
politikasıyla gece vardiyasının tamamını **sessizce ödenmez** hâle getirirdi: kayıt
yazılmaz (ya da yazılsa bile yön zincirine girmez), onay kuyruğuna düşmez, saate
sayılmaz — ve hiç kimse bir şey görmez. Bu, §4.6'nın ("kayıt asla kaybolmaz, sessiz
onay yok") **aynadaki hâlidir**: sessiz **iptal**. §4.6 nasıl "kanıt yetersizse
sessizce reddetme, kaydı yaz" diyorsa, buradaki simetrik kötüye kullanım "kaydı
sessizce hiç oluşturma"dır. `redirect` de aynı sebeple kapalıdır: bir tenant
"redirect" yazarak meşru tap'leri kayıt bırakmadan aktivasyona atabilirdi. Bu iki
effect ürünün varlık sebebine dokunduğu için tenant'a değil, yalnız değiştirilemez
guardrail katmanına aittir.

### 7. Beraberlik: spesifik kaynak genel kaynağı ezer

Baseline + tenant birlikte değerlendirilirken en kısıtlayıcı effect kazanır; ancak
**daha spesifik `resource` daha genel `resource`'u ezer**. Örneğin `location/rusty-bar`
kapsamlı bir `allow`, `location/*` kapsamlı bir `review`'u **yener**.

**Neden (Y-K).** "En kısıtlayıcı kazanır" tek başına dar istisna yazmayı
**imkânsız** kılardı. Müşteri "her yerde IP zorunlu, ama **yalnız** statik IP'si
olmayan Rusty Bar'da GPS yeterli" demek istediğinde, spesifik-ezer kuralı olmadan
`location/rusty-bar` `allow`'u genel `location/*` `review`'una hep yenilirdi. O
zaman müşterinin tek gevşetme aracı korumayı **9 şubenin hepsinde birden** kapatmak
olurdu — en makul (en dar) isteğin karşılığı en geniş (en tehlikeli) gevşetme
olamaz. Spesifiklik sıralaması sonucu **deterministik** kılar; eşit spesifiklikte
en kısıtlayıcı kazanır. (Guardrail bu kuralın **dışındadır**: guardrail terminaldir
ve hiçbir spesifik tenant `allow`'u onu yenemez — madde 4.)

### 8. Operatör / bağlam anahtarı / eylem / kaynak listeleri + genişletme kuralı

Aşağıdaki listeler v1 için normatiftir ve **kapalıdır**: doğrulama
([M3-03](../plan/m3-policy-motoru.md)) bu listelerin dışındaki her `effect`,
`action`, operatör veya bağlam anahtarını **doğrulama hatasıyla** reddeder — sessizce
yok saymaz. (Yok sayan motor, müşteriye "kuralım çalışıyor" yanılgısı verir; en
tehlikeli başarısızlıktır.)

**Operatörler (v1):** `StringEquals` · `StringNotEquals` · `StringIn` · `Bool` ·
`NumericEquals` · `NumericLessThan` · `NumericGreaterThan` · `IpInPrefix` · `Exists`
· `TimeBetween`.

**Bağlam anahtarları (v1, ad alanlı):**
`tap:channel` · `tap:sunValid` · `tap:ipMatch` · `tap:gpsMatch` · `tap:gpsDistanceM`
· `tap:gpsConflict` · `tap:trust` · `tap:ctrGap` · `tap:pageAgeSeconds` ·
`tap:practice` · `tap:direction` · `tap:queued` · `tap:occurredAtSkewSeconds` ·
`tag:status` · `tag:locationId` · `employee:status` · `employee:locationId` ·
`employee:departmentId` · `employee:crossLocation` · `location:id` · `time:localHour`
· `time:withinShift` · `time:minutesLate` · `actor:role`.

**Eylemler (v1):** `tap:record` · `tap:approve` · `record:manual` · `record:review`
· `employee:deactivate` · `report:export` · `policy:edit`.

**Kaynaklar:** `location/<id>` · `department/<id>` · `employee/<id>` · `*`. Kapsam
bağlama buradan yapılır — KF'nin 9 şubesi aynı politikayı paylaşmak zorunda değildir
(Rusty Bar gece vardiyası ile HQ ofisi aynı kuralı istemez).

**Genişletme kuralı: yeni operatör = yeni ADR.** Operatör kümesini büyütmek karar
motorunun ifade gücünü ve saldırı yüzeyini değiştirir; bu yüzden yeni bir operatör
eklemek bu ADR'ye ek bir ADR gerektirir, sessiz bir kod değişikliği değil. (Yeni
bağlam anahtarı/eylem eklemek de aynı disiplindedir; üçü de doğrulayıcının kapalı
listesini genişletir.)

**Üç yeni bağlam anahtarının gerekçesi** (plan denetiminde çıktı):

- **`tap:ctrGap`** = `ctr − last_ctr − 1`. Sıfırdan büyükse, gördüğümüz son tap'ten
  bu yana çipe kaç kez dokunulup **sunucuya gönderilmediğini** söyler. URL biriktirme
  saldırısının (A1) tek gözlemlenebilir izidir (Q21) — payload'da zaman olmadığı için
  başka sinyal yoktur. `base:ctr-gap-review` bu anahtara bağlanır, kaynak kapsamlı.
- **`tap:gpsConflict`** = GPS okundu **ama** lokasyondan uzak. `gpsMatch=false`
  bunu "GPS hiç yok" ile aynı kefeye koyuyordu; ikisi çok farklı şeylerdir: mekânda
  bırakılmış bir proxy üzerinden gelen tap'te (Y-E) IP eşleşir ama GPS çelişir — bu
  ancak ayrı bir anahtarla yakalanır.
- **`tap:occurredAtSkewSeconds`** = `created_at − occurred_at`, **sunucuda**
  hesaplanır. İstemcinin beyan ettiği zamanla gerçek varış arasındaki farktır; zaman
  dört kanıtın hiçbirinde olmadığı için (K1) mesai kaydının en kritik alanının tek
  doğrulama sinyalidir.

**Tüm bağlam anahtarları SUNUCUDA türetilir; istemci beyanından gelmez.** `tap:queued`,
`tap:practice` ve `tap:channel` dahil hepsi sunucu tarafında hesaplanır: `channel`
`ctr`/`cmac` varlığından, `practice` `employees.activated_at`'ten, `queued`
`occurredAtSkewSeconds`'tan türer. İstemcinin gövdede yolladığı bir bayrağa politika
bağlanırsa politika da istemcinin olur — `practice=true` yollayıp çıkış kaydını
zincirden düşürmek gibi (Y-İ). Bu ilke normatiftir: doğrulayıcı ve değerlendirici,
bağlamı yalnız sunucunun türettiği değerlerden alır.

### 9. Sürümleme: append-only

Politikalar **append-only**'dir: bir politikayı "düzenlemek" mevcut bir satırı
`UPDATE` etmek değil, **yeni bir sürüm** üretmektir. Her işlem kaydı, kendisine
karar veren politikanın **hangi sürümünü** kullandığını taşır (`policy_version_id`
— [M3-07](../plan/m3-policy-motoru.md)).

**Neden (§4.3 ile aynı gerekçe).** Mesai kaydı hukuki delil olabilir; §4.3
`transactions`'ı immutable kılar. Aynı mantık politika geçmişine de uzanır: bir
kayıt bugün "FLAGGED — policy: `gps-only-review`" diyorsa, o politika altı ay sonra
değişse bile o kaydın **neden** flag'lendiği değişmemelidir. Sürüm kaydı olmadan,
politika değişince geçmiş kaydın gerekçesi yeni kurala göre yanlış açıklanır. Bu
yüzden `policy_versions` tablosu da `REVOKE UPDATE, DELETE` + trigger ile korunur
([M3-02](../plan/m3-policy-motoru.md)) — `transactions` kalıbının aynısı. Politika
geçmişi de delil zinciridir.

### 10. Açıklanabilirlik zorunluluğu

Değerlendirici her karar için **hangi ifadenin** (`MatchedSid`) ve **hangi
katmanın** (`Layer` ∈ {guardrail, baseline, tenant}) karar verdiğini döndürür:

```go
type Decision struct {
    Effect          Effect
    MatchedSid      string
    PolicyVersionID *uuid.UUID
    Layer           Layer
    Reason          string
}
```

**Açıklanamayan karar üretilmez.** Hiç eşleşme olmasa bile sonuç
`MatchedSid = "default"` taşır (madde 3). Guardrail kararlarında `policy_version_id`
boştur ve `matched_sid = "sys:…"`'dır. Bu zorunluluk hem müdüre ("bu tap neden
flag'lendi") hem de audit'e cevap verilebilmesi için, hem de kararların kayda
bağlanabilmesi ([M3-07](../plan/m3-policy-motoru.md)) için gereklidir — gerekçe
serbest metin bir `note`'a değil, makine tarafından **filtrelenebilir** alanlara
yazılır ("geçen ay `gps-only-review` ile kaç kayıt").

### 11. Sınırlı parametre (bounded parameter)

Bazı korumalar **kapatılamaz ama aralık içinde ayarlanabilir**. Örnek: tap tazelik
penceresi ([M5-10](../plan/m5-tap-akisi.md)) tenant tarafından **kapatılamaz**, ama
süresi **1–15 dk** arasında seçilebilir. Motor sınır dışı bir değeri **reddeder**.
Diğer sınırlı parametreler ([M3-05](../plan/m3-policy-motoru.md)): debounce 30–300 sn,
`occurred_at` sapması 0–72 sa, GPS yarıçapı 25–1000 m.

**Neden bu kalıp.** Guardrail "aç/kapa" ikili değildir; bazı korumaların doğru
değeri müşteriye göre değişir (yoğun bir restoran ile hafta sonu ofisi aynı debounce
penceresini istemez) ama **hiçbiri sıfıra çekilememeli**. Sınırlı parametre,
guardrail'i esnetmeden gerçek dünyaya uyum sağlar: alt sınır korumanın anlamsız
hâle gelmesini, üst sınır ise korumanın fiilen kapanmasını engeller. Bu, aynı
sabitlerin `internal/config`'te de aralık kontrolünden geçmesini gerektirir
([M3-05](../plan/m3-policy-motoru.md) kabul kriteri): bugün `TAPPA_GPS_RADIUS_M`
yalnız `> 0` kontrolünden geçiyor, yani `TAPPA_GPS_RADIUS_M=20000000` tek satırlık
bir ortam değişikliğiyle tüm parkta *proof of place*'i sessizce kapatır (Y-L).
Guardrail'in ilan ettiği aralık config'te de zorlanmalı, yoksa koruma env
üzerinden delinir.

## Gerekçe

- **Neden bir motor, `if` zinciri değil.** §5'in `if` zinciri kırmızı çizgi ile
  tercihi aynı yerde tutar ve müşteriye kapalıdır. Kararları koddan **veriye**
  taşımak (versiyonlanabilir belge) üç şeyi mümkün kılar: müşteri kendi kuralını
  panelden açar (Q15–Q17), kırmızı çizgi guardrail katmanında ayrı ve
  gevşetilemez kalır (§4), ve her karar sürümüyle kayda bağlanır (§4.3). Kaybedilen
  şey — bir `if`'in doğrudanlığı — açıklanabilirlik ve gevşetilemezlik garantileriyle
  fazlasıyla telafi edilir.

- **Neden IAM'i taklit et, kendi DSL'ini yazma.** IAM modeli olgun, okunabilir ve
  denenmiştir; kaynak/eylem/koşul üçlüsü "kim neyi hangi bağlamda" sorusunun
  standart cevabıdır. Kendi dilimizi icat etmek yeni tuzaklar (öncelik belirsizliği,
  ayrıştırma köşe durumları) ve her tenant kuralı için yeniden öğrenme maliyeti
  getirirdi. IAM'den **sapılan** tek yerler ürünün gerçek farklarıdır: beş effect
  (madde 2), iki varsayılan (madde 3), gevşetilemez guardrail (madde 4) ve
  `ignore`/`redirect`'in tenant'a kapalılığı (madde 6).

- **Neden fail-to-review tap'te, fail-closed yetkilendirmede.** §4.6 tap için
  "kanıt yetersizse at**ma**, flag'le" der — yani tap tarafında güvenli yön
  *reddetmek değil, insana götürmektir*. Yetkilendirmede ise güvenli yön tam tersi:
  hiç kural bağlanmamışken "herkes yapabilir" felakettir (K2), o yüzden "hiç kimse
  yapamaz". İki farklı alan, iki farklı "güvenli varsayılan" ister; tek bir
  varsayılan ikisinden birini mutlaka yanlış yapar.

- **Neden guardrail sırası kodda ve normatif.** §5 "ilk eşleşen kazanır" bir
  **sıra** vaat eder; sıra veriye (DB'ye) bırakılırsa bir kayıt veya bir yanlış
  form onu bozabilir ve bilgi sızıntısı / push seli / replay penceresi açılır
  (madde 5). Sırayı kodda tek bir sıralı listede tutmak ve testle kilitlemek, bu
  sınırın disipline değil **yapıya** dayanmasını sağlar — ADR 0002'nin tenant
  çözümlemede vardığı sonucun aynısı.

- **Neden guardrail DB'de değil kodda.** DB'de tutulan bir guardrail, `tappa_app`
  veya `tappa_owner` üzerinden bir SQL erişimiyle kapatılabilirdi; §4 kırmızı
  çizgileri "politikayla delinemez" olmalı. Kodda gömülü + `sys:` ad alanı + devre
  dışı bırakma API'sinin yokluğu, bunu yapısal kılar ([M3-05](../plan/m3-policy-motoru.md)).

## Sonuçlar

- **[M3-02](../plan/m3-policy-motoru.md) (şema):** `policies`/`policy_versions`/
  `policy_attachments` tabloları; `layer ∈ {baseline, tenant}` (guardrail DB'de
  **yok** — madde 4). `policy_versions` append-only (`REVOKE UPDATE, DELETE` +
  trigger — madde 9). Üç tabloda da §6 RLS beşlisi tam.
- **[M3-03](../plan/m3-policy-motoru.md) (ayrıştırma/doğrulama):** madde 8'in kapalı
  listeleri (effect/action/operatör/anahtar) doğrulanır; bilinmeyen → hata, sessiz
  yok sayma yok. `ignore`/`redirect` baseline/tenant belgesinde reddedilir (madde 6).
  `sys:` ad alanı rezerve. Sınırlı parametre aralık dışıysa reddedilir (madde 11).
- **[M3-04](../plan/m3-policy-motoru.md) (değerlendirici):** saf fonksiyon
  (`time.Now()` yok, bağlam girdiden). Guardrail sıralı ve terminal (madde 5);
  baseline+tenant en kısıtlayıcı kazanır, spesifik kaynak geneli ezer (madde 7);
  eşleşme yoksa `tap:*`→`review`, diğerleri→`deny` (madde 3); her karar `MatchedSid`
  + `Layer` taşır (madde 10); sonuç deterministik.
- **[M3-05](../plan/m3-policy-motoru.md) (guardrail):** 1→10 normatif sıra (madde 5);
  §5 satır 1–5 ↔ `sys:*` eşlemesi (madde 4 tablosu); sınırlı parametreler config ile
  aynı aralıktan (madde 11).
- **[M3-06](../plan/m3-policy-motoru.md) (baseline):** §5 satır 6–7 + tespit edilen
  boşluklar (`base:ctr-gap-review`, `base:gps-conflict-review`, `base:qr-requires-ip`,
  …) varsayılan ama değiştirilebilir politikalar olarak (madde 4 tablosu).
- **[M3-07](../plan/m3-policy-motoru.md) (kayda bağlama):** `policy_version_id`,
  `matched_sid`, `policy_layer`, `policy_context jsonb` — 1. günden (madde 9, 10).
- **[M3-08](../plan/m3-policy-motoru.md) (test):** guardrail'in `allow`'a
  çevrilemezliği ve 1→10 sırası özellik/tablo testleriyle kanıtlanır (madde 4, 5).
- **[M3-09](../plan/m3-policy-motoru.md) → ADR 0005:** motorun **çözemediği**
  riskler (buddy punching, sahte GPS, URL biriktirme, mekânda proxy) ayrı ADR'de
  kabul edilir; bu ADR o risklerin sinyallerini üreten anahtarları (madde 8) tanımlar.
- **ADR 0002 ile bağ:** `sys:tenant-mismatch` guardrail'i (sıra 1) ADR 0002 madde
  7'deki tenant çözümleme istisnasının sonrasında akışı keser; bu ADR onu guardrail
  katmanına yerleştirir.
- **CLAUDE.md §5 ile tutarlı:** madde 4'ün eşleme tablosu §5 karar tablosunu
  katmanlara birebir bağlar; çelişki yok.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| **Kod içi `if` zinciri** (mevcut §5) | Doğru ama müşteriye kapalı: "GPS yeterli mi", "QR'da IP zorunlu mu" gibi tenant'a göre değişen kararları ifade edemez (Q15–Q17). Kırmızı çizgi ile tercihi aynı blokta tutar → birini gevşetirken diğerini açma riski. Karar veri değil kod olduğu için sürüm kaydı ve açıklanabilirlik (madde 9, 10) doğal değil. Motor bu zincirin **yapısını** korur ama veriye taşır. |
| **Tenant ayar tablosu** (boolean sütunlar: `gps_only_allowed`, `qr_requires_ip`, …) | Her yeni kural bir migration + kod dalı ister; kombinatoryal patlar. **Kaynak kapsamı ifade edilemez** — "yalnız Rusty Bar'da GPS yeter" (Y-K) bir boolean'a sığmaz, ya hep ya hiç olur. Sürümleme ve açıklanabilirlik yok: bir kaydın neden flag'lendiği, ayar o günden beri değiştiyse yeniden üretilemez. Sınırlı parametre (madde 11) bir boolean'la anlatılamaz. |
| **Rego / OPA** (Open Policy Agent) | **ADR 0001 "üçüncü parti/framework yok" ile çelişir:** ayrı bir runtime (OPA süreci veya gömülü değerlendirici) + yeni bir bağımlılık zinciri getirir; CLAUDE.md §1 "tek binary, stdlib > küçük kütüphane > framework" ethos'unu bozar. Rego Turing-tam'a yakın ifade gücüyle **doğrulanamaz genişlik** açar — guardrail'in gevşetilemezliğini (madde 4) ve nicel sınırları (Y-J) kanıtlamak zorlaşır. İki müşteri ve dar bir v1 yüzeyi (Q22) için aşırı ağır. Bizim kapalı-liste modelimiz (madde 8) kasıtlı olarak küçük ve doğrulanabilir. |
| **CEL** (Common Expression Language) | Rego'dan hafif ama yine **yeni bir bağımlılık ve yeni bir dil**: tenant'ın öğrenmesi, bizim güvenli alt kümesini sınırlamamız gereken bir ifade dili. Kapalı operatör listesi (madde 8) ve "yeni operatör = yeni ADR" disiplini, CEL'in açık ifade yüzeyinden daha denetlenebilir. CEL'in esnekliği v1'de gereksiz; guardrail gevşetilemezliğini ve determinizmi (madde 4, 7) kanıtlamak, kendi kapalı modelimizde daha kolay. Gerçekten gerekirse gelecekte ayrı bir ADR ile tartışılır. |
