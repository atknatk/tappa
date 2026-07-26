# M3 — Policy motoru (`internal/policy`)

**Amaç.** Kararların **kodda dağınık `if`'ler** yerine tek bir motordan çıkması.
Müşteri kendi kurallarını panelden açıp kapatabilsin, yenisini ekleyebilsin;
sistem ise ürünün kırmızı çizgilerini hiçbir müşterinin gevşetemeyeceği şekilde
korusun.

**Bittiğinde:** `policy.Evaluate(ctx) Decision` çalışıyor; `tap.Decide` bunun
üstünde duruyor; her işlem kaydı **hangi politikanın hangi ifadesiyle** karara
bağlandığını taşıyor; kapsam **%90+**.

> Motor **genel amaçlıdır**, yalnız tap'e ait değil. Aynı motor şunlara da bakar:
> FLAGGED onayını kim verebilir, manuel kayıt kim girebilir, rapor kim dışa
> aktarabilir, hangi rolde ne yapılabilir. Bu yüzden `action` ad alanlı.

## v1 kapsamı — bilinçli olarak dar (Q22)

Dış denetim uyarısı haklı: iki müşteri ve ~100 kullanıcı için tam bir IAM
motoru + JSON editörü + simülatör, pilotu geciktirir ve ürünün doğruluk
çekirdeğini (M4) genel amaçlı bir motorun doğruluğuna bağımlı kılar.

**Motor mimarisi korunuyor, v1 yüzeyi daraltılıyor:**

| v1'de VAR | v1'de YOK → ertelendi |
|---|---|
| Guardrail katmanı (M3-05) | Ham JSON politika editörü → [M9-07](m9-sonrasi.md) |
| Baseline politikaları (M3-06) | Policy simülatörü → [M9-06](m9-sonrasi.md) |
| Değerlendirici + açıklanabilirlik (M3-04, M3-07) | Tenant'ın sıfırdan belge yazması |
| Sık kullanılan kurallar için **form** UI ([M6-09](m6-dashboard.md)) | |

Yani v1'de tenant, baseline politikalarını **form üzerinden açar/kapatır ve
parametrelerini ayarlar**; belge modeli, sürümleme ve değerlendirici tam olarak
kurulur ama serbest belge yazımı pilot sonrasına kalır. `policy_context`
anlık görüntüsü (M3-07) **1. günden** yazılır — simülatör sonra gelse de
geçmişi yeniden üretmek mümkün olsun.

---

## Model — AWS IAM'den ne alındı, ne değiştirildi

| Konu | AWS IAM | Tappa | Neden |
|---|---|---|---|
| Belge yapısı | `Statement[]` · `Effect` · `Action` · `Resource` · `Condition` | **aynı** | Bilinen, denenmiş, okunabilir |
| Effect kümesi | `Allow` \| `Deny` | `allow` \| `review` \| `deny` \| `ignore` \| `redirect` | Tappa'nın sonucu iki değil beş: `ok` · `flag` · `reject` · `ignored` · aktivasyona yönlendir (kayıt yok) |
| Öncelik | explicit Deny > Allow | `deny` > `ignore` > `review` > `allow` | En kısıtlayıcı kazanır. `ignore` ve `redirect` **yalnız guardrail** üretebilir (aşağı bak) |
| Eşleşme yoksa | **implicit deny** | eyleme göre: **`tap:*` → `review`**, diğer her eylem → **`deny`** | §4.6 yalnız tap için geçerli: kanıt yetersizse kayıt atılmaz, `flag`'lenir. Ama `policy:edit` veya `report:export` için "review" anlamsızdır — orada **fail-closed** olmak zorundayız |

**`ignore` ve `redirect` tenant'a kapalı.** Bir tenant `ignore` yazabilseydi
*"22:00'den sonraki her tap `ignore`"* politikasıyla gece vardiyasının tamamını
sessizce ödenmez hâle getirirdi: kayıt yazılır (§4.6 sağlanır) ama yön zincirine
girmez, onay kuyruğuna düşmez, saate sayılmaz. Bu §4.6'nın aynadaki hâlidir —
sessiz **iptal**. M3-03 doğrulaması bu iki effect'i `baseline` ve `tenant`
belgelerinde **reddeder**.

**Eylem ad alanına göre iki farklı varsayılan.** `tap:record` için eşleşme yoksa
`review` (fail-to-review, §4.6). Yetkilendirme eylemleri — `policy:edit`,
`report:export`, `tap:approve`, `record:manual`, `record:review`,
`employee:deactivate` — için eşleşme yoksa **`deny`** (fail-closed). Aksi hâlde
hiç yetki politikası bağlanmamış yeni bir tenant'ta (M7-03 provisioning)
**herkes politika düzenleyebilir ve rapor dışa aktarabilir** olurdu.
| Gevşetme | daha geniş bir Allow eklenir | **eklenmez** — kısıtlayıcı belge devre dışı bırakılır/değiştirilir | "En kısıtlayıcı kazanır" ile çelişmesin; gevşetme bilinçli bir eylem olsun |
| Koruma | SCP / permission boundary | **guardrail katmanı** (değiştirilemez) | §4 kırmızı çizgileri politikayla delinemez |

### Üç katman

```
1. guardrail   sistem, DEĞİŞTİRİLEMEZ, kapatılamaz.   §4 + §5 satır 1–5.
               Eşleşirse TERMİNAL — alt katmanlar hiç çalışmaz.
2. baseline    Tappa'nın yönetilen politikası, her tenant'a varsayılan bağlı.
               §5 satır 6–7. Tenant devre dışı bırakabilir veya kendi
               sürümüyle değiştirebilir.
3. tenant      Müşterinin yazdığı politikalar.
```

Değerlendirme: guardrail → (eşleşme yoksa) baseline + tenant birlikte, **en
kısıtlayıcı effect kazanır** → hiç eşleşme yoksa **`review`**.

### Belge biçimi

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
      "condition": {
        "Bool": { "tap:ipMatch": false, "tap:gpsMatch": true }
      },
      "reason": "verified via GPS only — no network proof of place"
    }
  ]
}
```

**Operatörler (v1, bilinçli olarak küçük):** `StringEquals` · `StringNotEquals` ·
`StringIn` · `Bool` · `NumericEquals` · `NumericLessThan` · `NumericGreaterThan` ·
`IpInPrefix` · `Exists` · `TimeBetween`. Yeni operatör eklemek ADR ister.

**Bağlam anahtarları (v1, ad alanlı):**
`tap:channel` · `tap:sunValid` · `tap:ipMatch` · `tap:gpsMatch` ·
`tap:gpsDistanceM` · **`tap:gpsConflict`** · `tap:trust` · **`tap:ctrGap`** ·
`tap:pageAgeSeconds` · `tap:practice` · `tap:direction` · `tap:queued` ·
**`tap:occurredAtSkewSeconds`** ·
`tag:status` · `tag:locationId` ·
`employee:status` · `employee:locationId` · `employee:departmentId` ·
`employee:crossLocation` ·
`location:id` · `time:localHour` · `time:withinShift` · `time:minutesLate` ·
`actor:role`

Üç anahtarın gerekçesi (denetimde çıktı, bkz. [open-questions.md](open-questions.md)):
- **`tap:ctrGap`** = `ctr − last_ctr − 1`. Sıfırdan büyükse, gördüğümüz son
  tap'ten bu yana çipe kaç kez dokunulup **gönderilmediğini** söyler. URL
  biriktirmenin tek gözlemlenebilir izi budur (Q21).
- **`tap:gpsConflict`** = GPS okundu **ama** lokasyondan uzak. `gpsMatch=false`
  bunu "GPS hiç yok" ile aynı kefeye koyuyordu; ikisi çok farklı şeyler.
  Mekânda bırakılmış bir proxy üzerinden gelen tap'te IP eşleşir, GPS çelişir.
- **`tap:occurredAtSkewSeconds`** = `created_at − occurred_at`, **sunucuda**
  hesaplanır. İstemcinin beyan ettiği zamanla gerçek arasındaki fark.

**Bu anahtarların hiçbiri istemci beyanından gelmez.** `tap:queued`,
`tap:practice` ve `tap:channel` dahil hepsi sunucuda türetilir — `channel`
`ctr`/`cmac` varlığından, `practice` `employees.activated_at`'ten,
`queued` `occurredAtSkewSeconds`'tan. İstemcinin gövdede yolladığı bir bayrağa
politika bağlanırsa politika da istemcinin olur.

**Eylemler (v1):** `tap:record` · `tap:approve` · `record:manual` ·
`record:review` · `employee:deactivate` · `report:export` · `policy:edit`

**Kaynaklar:** `location/<id>` · `department/<id>` · `employee/<id>` · `*`
Kapsam bağlama buradan yapılır — KF'nin 9 şubesi aynı politikayı paylaşmak
zorunda değil (Rusty Bar gece vardiyası ile HQ ofisi aynı kuralı istemez).

### Sınırlı parametre (bounded parameter)

Bazı korumalar **kapatılamaz ama ayarlanabilir**. Örnek: tap tazelik penceresi
(→ [M5-10](m5-tap-akisi.md)) kapatılamaz; süresi tenant tarafından **1–15 dk**
arası seçilebilir. Motor sınır dışı değeri reddeder. Bu kalıp guardrail'i
esnetmeden gerçek dünyaya uyum sağlar.

---

## M3-01 — ADR 0004: policy motoru modeli

- **Bağımlılık:** M1-01
- **Kırmızı çizgi:** §4 (tamamı — guardrail katmanının varlık sebebi)
- **Commit:** `docs(adr): record the policy engine model`

**Amaç.** Yukarıdaki modeli gerekçeleriyle karara bağlamak.

**İçermesi gerekenler.**
1. Neden IAM benzeri belge yapısı, neden beş effect, ve **neden iki farklı
   varsayılan**: `tap:*` için `review` (fail-to-review, §4.6), yetkilendirme
   eylemleri için `deny` (fail-closed).
1b. Guardrail'lerin **sıralı** olduğu ve sıranın normatif olduğu (M3-05 tablosu);
   yanlış sıranın somut sömürüsü (bilgi sızıntısı, push seli, replay penceresi).
1c. `ignore`/`redirect` effect'lerinin tenant'a kapalı olduğu ve gerekçesi.
1d. Beraberlik çözümünde **spesifik kaynağın genel kaynağı ezdiği** (Y-K).
2. Üç katman ve guardrail'in **hiçbir koşulda** gevşetilemeyeceği.
3. Operatör/anahtar/eylem listesi ve genişletme kuralı (yeni operatör = ADR).
4. Sürümleme: politikalar **append-only**; düzenleme yeni sürüm üretir. Her
   işlem kaydı kullanıldığı sürümü taşır → geçmiş kayıt sonradan da açıklanabilir
   (§4.3 ile aynı gerekçe: mesai kaydı hukuki delil olabilir).
5. Açıklanabilirlik zorunluluğu: her karar hangi `sid`'in verdiğini döndürür.
6. Sınırlı parametre kalıbı.
7. Değerlendirilen alternatifler: kod içi `if` zinciri (mevcut §5), tenant
   ayar tablosu (boolean sütunlar), Rego/OPA (ek bağımlılık + ayrı runtime),
   CEL. Neden seçilmedikleri.

**Kabul kriterleri.**
- ADR yazıldı, "kabul edildi", tarihli.
- CLAUDE.md §5 ile çelişmiyor: §5 satır 1–5 guardrail, satır 6–7 baseline olarak
  açıkça eşleştirilmiş.
- M3-03 ve M3-04 bu ADR'den tek yorum çıkararak yazılabiliyor.

---

## M3-02 — Policy şeması

- **Bağımlılık:** M3-01 · M1-06
- **Kırmızı çizgi:** §4.3 (append-only) · §4.5 (tenant izolasyonu)
- **Araç:** agent `tappa-db-migrator`
- **Commit:** `feat(db): add policy documents and attachments`

**Tablolar.**
```
policies(id, tenant_id, name, layer, enabled, created_at, created_by)
        -- layer: baseline | tenant   (guardrail DB'de DEĞİL, kodda)
policy_versions(id, tenant_id, policy_id, version_no, document jsonb,
                created_at, created_by)          -- APPEND-ONLY
policy_attachments(id, tenant_id, policy_id, resource, created_at)
```

**Kabul kriterleri.**
- Üç tabloda da RLS beşlisi tam (`tenant_id` + indeks + ENABLE/FORCE + USING/
  WITH CHECK + GRANT).
- `policy_versions` append-only: `REVOKE UPDATE, DELETE` + trigger (M1-06'daki
  `transactions` kalıbının aynısı). Politika geçmişi de delil zinciridir.
- `document jsonb` üzerinde şema doğrulaması **uygulamada** (M3-03), DB'de
  serbest — ileri sürüm uyumu için.
- `policies.enabled` tenant'ın aç/kapa anahtarı; kapatmak **silmek değildir**.
- **Guardrail'ler DB'de tutulmaz.** Kodda gömülüdür; DB'ye yazılabilseydi
  bir SQL erişimi kırmızı çizgileri kapatabilirdi.

**Tuzaklar.**
- `policy_versions`'ı `UPDATE` ile "düzeltmek" cazip; yasak. Yeni sürüm yaz.

---

## M3-03 — Belge modeli, ayrıştırma ve doğrulama

- **Bağımlılık:** M3-01
- **Commit:** `feat(policy): parse and validate policy documents`

**Dokunulacak dosyalar.** `internal/policy/document.go`, `validate.go`, testleri

**Kabul kriterleri.**
- Bilinmeyen `effect`, `action`, operatör veya bağlam anahtarı → **doğrulama
  hatası**, sessizce yok sayma yok. (Yok sayan motor, müşteriye "kuralım çalışıyor"
  yanılgısı verir — en tehlikeli başarısızlık.)
- `sys:` ad alanı rezerve: tenant belgesi bu ad alanına dokunamaz.
- **`ignore` ve `redirect` effect'leri `baseline`/`tenant` belgelerinde reddedilir**
  — yalnız guardrail üretebilir. Gerekçe modelde.
- Sınırlı parametreler aralık dışındaysa reddedilir.
- **Nicel sınırlar zorunlu:** belge boyutu, belge başına ifade sayısı, ifade
  başına `action`/`resource`/`condition` sayısı, `IpInPrefix` liste uzunluğu,
  tenant başına belge ve sürüm kotası. Sınır aşımı doğrulama hatası.
  Gerekçe: `Evaluate` **her tap'te** çalışıyor ve mimari **tek VPS, tek process**
  (CLAUDE.md §1). 200 belge × 500 ifade yazan bir tenant, vardiya başlangıcında
  tüm tenant'lar için servisi durdurabilir; `transactions` immutable olduğu için
  o sırada oluşmayan kayıt geri de getirilemez. `policy_versions` append-only
  olduğundan kotasız hâlde disk de doldurulabilir.
- Bozuk/kısmi JSON → anlamlı hata, panik yok; fuzz testi koşuldu.
- Doğrulama **yazma anında** yapılır; değerlendirme anında belge zaten geçerlidir.

---

## M3-04 — Değerlendirici

- **Bağımlılık:** M3-03
- **Commit:** `feat(policy): evaluate policies with explainable decisions`

**Dokunulacak dosyalar.** `internal/policy/evaluate.go`, `conditions.go`, testleri

**İmza.**
```go
type Decision struct {
    Effect          Effect // allow | review | deny | ignore | redirect
    MatchedSid      string // hangi ifade karar verdi
    PolicyVersionID *uuid.UUID
    Layer           Layer  // guardrail | baseline | tenant
    Reason          string
}

func Evaluate(set Set, ctx Context) Decision
```

> **Kart düzeltmesi (2026-07-26, M3-04 uygulaması).** İki düzeltme:
> 1. `Decision.Effect` yorumu effect'leri `allow | review | deny | ignore`
>    sayıp **`redirect`'i atlıyordu**. Beş effect vardır (ADR 0004 §2) ve
>    değerlendirici `redirect` döndürür: guardrail'ler `sys:no-session` ve
>    `sys:tenant-mismatch` (M3-05) redirect üretir. Yorum düzeltildi.
> 2. Aşağıdaki "Hiç eşleşme yok → `tap:*` için `review`" ifadesi ADR 0004 §3
>    ile birebir değil: ADR §3 `tap:approve`'u **fail-closed `deny`** listesine
>    koyar (M3-08 testi de öyle). `tap:*` bir kısaltmadır; kesin kural **tap
>    KAYDI** eylemi (`tap:record`) → `review`, **diğer her eylem** (`tap:approve`
>    dâhil) → `deny`. Uygulama (`reviewDefaultActions = {tap:record}`) ADR §3'ü
>    izler; kabul kriteri metni de buna göre okunmalı.

**Kabul kriterleri.**
- Saf fonksiyon: DB, HTTP, `time.Now()` **yok** — bağlam girdiden gelir
  (`internal/domain/tap` ile aynı disiplin).
- Guardrail'ler **sıralı** değerlendirilir (M3-05'teki 1→10 sırası), ilk eşleşen
  terminaldir; alt katmanlar çalıştırılmaz.
- Baseline + tenant birlikte, en kısıtlayıcı kazanır: `deny` > `ignore` >
  `review` > `allow`. Beraberlikte **daha spesifik `resource`** kazanır ve
  sonuç deterministiktir.
- **Spesifik kaynak, genel kaynağı ezebilir.** `location/rusty-bar` kapsamlı bir
  `allow`, `location/*` kapsamlı bir `review`'u yener. Aksi hâlde "her yerde IP
  zorunlu, **yalnız** statik IP'si olmayan Rusty Bar'da GPS yeterli" ifade
  edilemezdi ve müşterinin tek gevşetme aracı korumayı **9 şubenin hepsinde**
  kapatmak olurdu — en makul isteğin karşılığı en geniş gevşetme olamaz.
- Hiç eşleşme yok → **`tap:record`** için `review` (tap kaydı, §4.6), **diğer
  her eylem** (`tap:approve` dâhil — ADR 0004 §3 authz listesi) için `deny`;
  `MatchedSid = "default"`. (Kart başındaki düzeltme bloğu 2. maddesine bakın.)
- **Bilinmeyen operatör/anahtar değerlendirme anında** (sürüm geri alma sonrası
  mümkün): ifade **eşleşmez** ve olay loglanır. Koşulu atlayıp ifadeyi eşleştirmek
  bir `deny` politikasını koşulsuz hâle getirir — tüm tap'ler reddedilir.
- Her karar `MatchedSid` ve `Layer` taşıyor — **açıklanamayan karar üretmiyor**.
- Değerlendirme sırası ve sonucu **deterministik** (aynı girdi → aynı çıktı,
  map iterasyon sırasına bağımlılık yok).

**Tuzaklar.**
- Go'da `map` iterasyonu rastgeledir; ifade sıralaması sabitlenmezse beraberlik
  çözümü oturumdan oturuma değişir ve simülasyon güvenilmez olur.
- Eksik bağlam anahtarı ≠ `false`. Yoksa `Exists` dışındaki operatörler
  **eşleşmez** — bu ayrım testli olmalı.

---

## M3-05 — Guardrail politikaları

- **Bağımlılık:** M3-04
- **Kırmızı çizgi:** §4.1 … §4.7 — **bu milestone'un en kritik görevi**
- **Commit:** `feat(policy): add immutable system guardrails`

**Amaç.** Ürünün varlık sebebini politikayla delinemez hale getirmek.

**Guardrail listesi — SIRA NORMATİFTİR, ilk eşleşen kazanır ve terminaldir.**
Kodda gömülü, `sys:` ad alanı.

| # | sid | Kural | Kaynak |
|---|---|---|---|
| 1 | `sys:tenant-mismatch` | Etiketin tenant'ı ≠ oturumun tenant'ı → `redirect` (kayıt yok) | §4.5 · Y2 |
| 2 | `sys:tag-not-active` | Etiket `retired` → `deny`; `lost` → `deny` **+ güvenlik uyarısı** | §5 satır 1 |
| 3 | `sys:sun-invalid` | CMAC uyuşmuyor veya `ctr` artmadı → `deny` | §5 satır 2 · §4.4 |
| 4 | `sys:tap-freshness` | Sayfa yaşı pencereyi aştı → `deny` | [M5-10](m5-tap-akisi.md) |
| 5 | `sys:occurred-at-bound` | `occurred_at` gelecekte, veya sapma üst sınırı aştı → `deny` | K1 |
| 6 | `sys:no-session` | Oturum yok → `redirect`, **kayıt yok** | §5 satır 3 |
| 7 | `sys:employee-deactivated` | `deactivated` → `deny` + güvenlik uyarısı | §5 satır 4 |
| 8 | `sys:person-debounce` | Aynı kişi pencere içinde → `ignore` | §5 satır 5 |
| 9 | `sys:policy-edit-owner-only` | `policy:edit` yalnız tenant owner'ında → aksi `deny` | K2 |
| 10 | `sys:no-self-review` | `reviewer_id == transaction.employee_id` → `deny` | Y-C |

**Sıra neden normatif:** §5 "ilk eşleşen kazanır" der ve sıralama sömürülebilir.
`sys:employee-deactivated`, `sys:sun-invalid`'den **önce** gelseydi: elinde eski
bir URL ve çalınmış çerez olan biri hesabın deaktive olup olmadığını öğrenirdi
(bilgi sızıntısı) **ve** deaktive karar güvenlik uyarısı ürettiği için müdürün
telefonuna sınırsız push yollayabilirdi (uyarı yorgunluğu → gerçek olay gürültüde
kaybolur). `sys:person-debounce`, `sys:sun-invalid`'den önce gelseydi replay
penceresi açılırdı. Bu iki hatayı agent `tappa-security-auditor` R8 arar.

**Guardrail değil, invariant** (motorun döndürdüğü bir effect değil, çağıranın
uyması gereken kural — ayrı test edilir, [M3-08](m3-policy-motoru.md)):
`sys:never-lose-record` (§4.6, yazma yolu M5-05'te) ·
`sys:no-biometrics` (§4.1, hiçbir biyometrik bağlam anahtarı **tanımlı değil**).

**Kabul kriterleri.**
- Guardrail'ler DB'den okunmuyor, kodda sabit; devre dışı bırakma API'si **yok**.
- **Sıra kodda tek bir yerde, sıralı liste olarak** tanımlı; testi M3-08'de.
- Sınırlı parametreler: debounce 30–300 sn, tazelik penceresi 1–15 dk,
  `occurred_at` sapması 0–72 sa, GPS yarıçapı 25–1000 m. Aralık dışı reddediliyor.
- **`internal/config/config.go` aynı sabitlerden okuyor.** Bugün
  `TAPPA_GPS_RADIUS_M` ve `TAPPA_DEBOUNCE_SECONDS` yalnız `> 0` kontrolünden
  geçiyor: `TAPPA_GPS_RADIUS_M=20000000` tek satırlık bir ortam değişikliğiyle
  tüm parkta *proof of place*'i sessizce kapatır. Alt/üst sınır dışı değer
  **başlangıçta hata** (config paketi zaten bu disiplinde).
- `sys:person-debounce` **kişi** bazlı — etiket bazlı değil (§5).
- `sys:tenant-mismatch`: KM çalışanı KF plaketine dokunduğunda kayıt **hiçbir**
  tenant'a yazılmaz; KF müdürü KM çalışanının adını/GPS'ini görmez (§4.5).

---

## M3-06 — Tappa Baseline yönetilen politikası

- **Bağımlılık:** M3-05
- **Commit:** `feat(policy): ship the tappa baseline managed policy`

**Amaç.** §5 satır 6–7'yi ve tespit edilen boşlukları **varsayılan ama
değiştirilebilir** politikalar olarak paketlemek.

**Baseline ifadeler.**

| sid | Kural | Karşılığı |
|---|---|---|
| `base:ip-or-gps-ok` | IP eşleşti **veya** GPS yarıçapta → `allow` | §5 satır 6 |
| `base:no-evidence-review` | İkisi de yok → `review` | §5 satır 7 |
| `base:qr-requires-ip` | `tap:channel=qr` **ve** `tap:ipMatch=false` → `review` | **Q15 ✅** — QR'da GPS yetmez |
| `base:gps-only-allow` | `ipMatch=false` **ve** `gpsMatch=true` → `allow`, not `verified via GPS` | **Q16 ✅** — tenant tek anahtarla `review`'a çeker |
| `base:cross-location-note` | Tap lokasyonu ≠ profil lokasyonu → `allow` + not | **Q17 ✅** — vardiya tap edilen lokasyondan |
| `base:queued-window` | `tap:occurredAtSkewSeconds` eşiği aşıldı → `review` | Y7 (üst sınır guardrail'de) |
| `base:ctr-gap-review` | `tap:ctrGap > 0` → `review` | **Q21 ✅** — A1'in tek gerçek sinyali |
| `base:gps-conflict-review` | `tap:gpsConflict = true` → `review` | Y-E — mekânda proxy |

**Kabul kriterleri.**
- Her yeni tenant'a otomatik bağlanıyor (M7-03 provisioning) — **yetkilendirme
  politikaları dahil**, aksi hâlde fail-closed varsayılanı yeni tenant'ta hiç
  kimsenin panele giremediği anlamına gelir.
- `base:ctr-gap-review` **kaynak kapsamlı** kurulur: yoğun şubede (KF St Julians)
  tenant kapatabilir, düşük trafikli yerde (Rusty Bar, HQ, hafta sonu) açık
  kalır — URL biriktirme zaten yalnız orada işe yarıyor (Q21).
- Tenant devre dışı bırakabiliyor veya kendi sürümüyle değiştirebiliyor;
  guardrail'i **etkilemiyor**.
- `base:` sid'leri koruma altında: tenant aynı sid ile çakışan belge yazamaz,
  kendi sid'ini kullanır.
- Baseline sürümü yükseldiğinde mevcut tenant'lar **otomatik güncellenmiyor** —
  bildirilir, tenant onaylar. (Sessiz kural değişikliği mesai raporunu değiştirir.)

---

## M3-07 — Kararın kayda bağlanması

- **Bağımlılık:** M3-04 · M1-06
- **Kırmızı çizgi:** §4.3
- **Commit:** `feat(policy): record which policy decided each transaction`

**Amaç.** Her işlem kaydının "neden bu sonucu aldığını" taşıması.

**Kabul kriterleri.**
- `transactions` üzerine `policy_version_id`, `matched_sid`, `policy_layer` ve
  **`policy_context jsonb`** alanları (M1-06'ya ek migration; uygulanmış
  migration değiştirilmez).
- **`policy_context` = kararın verildiği andaki bağlam anlık görüntüsü.**
  Değerlendirmeye giren tüm anahtarlar (`tap:ctrGap`, `tap:gpsDistanceM`,
  `tap:pageAgeSeconds`, o anki `employee:status`, `time:minutesLate` …).
  Bu sütun **1. günden itibaren** yazılır; sonradan eklenemez çünkü geçmiş
  yeniden üretilemez. Olmadan: (a) simülatör ([M9-06](m9-sonrasi.md)) geçmiş
  tap'leri yeniden değerlendiremez, (b) vardiya tanımı veya çalışanın profili
  sonradan değişmişse "neden FLAGGED" sorusunun cevabı yanlış olur.
- Docket kartı "FLAGGED — policy: `gps-only-review`" gösterebiliyor (M6-04).
- Politika sonradan değişse bile **geçmiş kayıt aynı gerekçeyi** gösteriyor —
  sürüm kaydı sayesinde.
- Guardrail kararlarında `policy_version_id` boş, `matched_sid = "sys:…"`.

**Tuzaklar.**
- Gerekçeyi serbest metin `note` alanına gömmek yetmez; makine tarafından
  filtrelenebilir olmalı (rapor: "geçen ay `gps-only-review` ile kaç kayıt").

---

## M3-08 — Test seti ve gevşetilemezlik kanıtı

- **Bağımlılık:** M3-05 · M3-06 · M3-07
- **Kırmızı çizgi:** §8
- **Commit:** `test(policy): prove guardrails cannot be loosened`

**Kabul kriterleri.**
- Tablo bazlı testler: her operatör, her effect, her katman.
- **Özellik testi:** rastgele üretilmiş **hiçbir** tenant politikası, guardrail'in
  `deny`/`ignore`/`redirect` kararını `allow`'a çeviremiyor. Pazarlıksız.
- **Guardrail sırası testi:** M3-05'teki 1→10 sırası tablo bazlı doğrulanıyor;
  özellikle `sys:sun-invalid` her zaman `sys:employee-deactivated` ve
  `sys:person-debounce`'tan **önce** çalışıyor.
- Hiç politika bağlı değilken: `tap:record` → `review`; `policy:edit`,
  `report:export`, `tap:approve`, `record:manual` → **`deny`**. Bu ayrım testli.
- `ignore`/`redirect` içeren bir tenant/baseline belgesi **doğrulamada reddediliyor**.
- Nicel sınırları aşan belge reddediliyor; sınır testleri var.
- **Invariant testleri** (guardrail değil, ayrı): kanıt yetersizliğinde kayıt
  yazılıyor (§4.6) · hiçbir bağlam anahtarı biyometrik veri taşımıyor (§4.1).
- Bozuk/çelişkili politika seti → deterministik ve **daha kısıtlayıcı** sonuç.
- `internal/policy` kapsamı **%90+**.
- Denetim: agent `tappa-security-auditor` — guardrail bypass'ı ve `sys:` ad
  alanı sızıntısı özellikle aranıyor.

---

## M3-09 — ADR 0005: kabul edilen riskler

- **Bağımlılık:** M3-05
- **Kırmızı çizgi:** §4.1 (biyometri yasağı — bu ADR'nin sınırı)
- **Commit:** `docs(adr): record accepted risks and their detection signals`

**Amaç.** Policy motorunun ve dört kanıtın **çözemediği** şeyleri yazılı olarak
kabul etmek, her biri için tespit sinyalini ve satış cevabını belirlemek.

**Neden.** Yazılı olmayan kabul, unutulmuş açık demektir. Ayrıca müşteri ilk
demoda "peki telefonunu arkadaşına verirse?" diye soracak — cevabımızın hazır,
dürüst ve tutarlı olması gerekiyor.

**İçermesi gerekenler.**

| Risk | Neden çözemiyoruz | Tespit sinyali |
|---|---|---|
| **Buddy punching** (A4/Q19) | Çözümü biyometri veya uygulama kurulumu; ikisi de ürünün varlık sebebine aykırı (§4.1, app-less vaadi) | Eş-zamanlı tap çiftleri raporu ([M6-11](m6-dashboard.md)) |
| **Sahte GPS** (A3) | Web'de konum doğrulaması yok; attestation uygulama ister | GPS-only tap oranı, çalışan kırılımında ([M6-11](m6-dashboard.md)) |
| **URL biriktirme** (A1/Q21) | SUN payload'ında zaman yok. Uçak modunda 10 kez dokunup URL'leri toplamak mümkün; sunucu o okumaları hiç görmez. Düşük trafikli plakette çalışır (yoğun plakette biriktirilen URL'ler başkasının tap'iyle ölür) | `tap:ctrGap` → `base:ctr-gap-review`; boşluk metriği ([M6-11](m6-dashboard.md)) |
| **Mekânda bırakılmış proxy** (Y-E) | IP taşınabilir. Mekânın WiFi'ında prizde duran 20 €'luk telefon + VPN = evden gelen tap'in kaynak IP'si mekânın IP'si. Kriptografik çözümü yok | `tap:gpsConflict` → `base:gps-conflict-review`; "IP eşleşti ama GPS uyuşmuyor" metriği. **Uyarı:** bu saldırı GPS-only oranında görünmez, tersine kayıtları "IP ile doğrulanmış" olarak en güvenilir gösterir |
| **Müdürün kimlik basması** (Y-D) | Davet kodunu üreten ve gören kişi ile bordroyu şişirmekte en güçlü teşviki olan kişi aynı. Sahte profil aç → kodu oku → kendi telefonunun ikinci tarayıcı profilinde aktive et → her gün trust 100, gerçek çalışandan ayırt edilemez | "Tek cihaz/oturum kaynağından aktive edilmiş N çalışan" + "hiç çapraz-lokasyon göstermeyen çalışan" ([M6-11](m6-dashboard.md)); Q02 çözülünce davet kodu **çalışanın kendi kanalına** gider ([M5-02](m5-tap-akisi.md)) |
| **Fiziksel plaket devri** | Plaket sökülüp taşınabilir | Lokasyon–IP/GPS uyumsuzluğu → `flag`; müdür `lost` işaretler |

**Satış cevabının çekirdeği (buddy punching).** 1 oturum = 1 çalışan — bir
telefon iki kişiyi basamaz, karta göre daha zor · plaket kamera görüş alanında ·
telefonun kendi ekran kilidi gönülsüz devri engeller · eş-zamanlı tap çiftleri
raporlanır. **Dürüst kalan taraf:** gönüllü iş birliğini hiçbiri durdurmaz.
Karşılığında enrollment, cihaz arızası, ıslak el ve GDPR yükünden kurtuluyoruz.

**Kabul kriterleri.**
- ADR yazıldı, "kabul edildi", tarihli; her risk için tespit sinyali **ve**
  hangi görevde uygulandığı yazılı.
- Hiçbir risk "ileride bakarız" ile geçiştirilmemiş — ya kabul, ya görev.
- Satış cevabı [docs/handoff.md](../handoff.md) §2'ye referansla tutarlı:
  parmak izinin çözdüğü tek şeyin bu olduğu **açıkça** kabul ediliyor.
- Yeni bir kabul edilen risk çıkarsa bu ADR'ye eklenir (append), yenisi açılmaz.
