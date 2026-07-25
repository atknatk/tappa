# ADR 0002 — Tenant bağlamı ve RLS stratejisi

- **Durum:** kabul edildi
- **Tarih:** 2026-07-25

> **ADR güncellemesi (2026-07-25, M1-04 uygulaması sırasında).** Bağımsız bir
> tasarım denetçisi madde 7'nin reddettiği "GUC-anahtar saf-RLS" alternatifini
> canlı Postgres'te sınadı. **Kararın kendisi (çevrelenmiş bypass) DEĞİŞMEDİ ve
> doğrulandı** — düzeltilen yalnızca madde 7'nin bir gerekçesidir. İki bulgu
> normatiftir:
>
> 1. **Madde 7'nin "saf RLS bunu ifade edemez / bypass kaçınılmaz" önermesi
>    teknik olarak YANLIŞTI ve geri çekildi.** Bir resolve-GUC'a
>    (`app.resolve_token` gibi) anahtarlanmış RLS dalı çözümlemenin mutlu-yolunu
>    **bypass'sız** ifade eder: `tappa_app` tam RLS'e tabidir, dönüş tek anahtarla
>    sınırlıdır, genel bypass yoktur. Madde 7'nin "bağlamı bilinmeyen satırı
>    okumanın üç yolu, üçü de bypass içerir" listesi bu dördüncü yolu atladığı için
>    eksikti.
> 2. **Ama alternatif yine de REDDEDİLDİ, çünkü çevrelemesi disipline dayanır,
>    YAPIYA değil.** İki tek-nokta hatası canlıda çapraz-tenant ihlal üretti:
>    (a) **okuma:** resolve-GUC `SET LOCAL`'sız kurulursa havuz bağlantısında
>    kalır; toplamsal OR-dalı o bağlantıdaki *her* meşru tenant sorgusuna bir
>    çapraz-tenant satır ekler; `NULLIF` fail-closed bunu **yakalayamaz** ve
>    `WithTenant` `app.tenant_id`'yi ezse de resolve-GUC'a dokunmaz. (b) **yazma:**
>    `FOR ALL USING(...)` kısayolu `WITH CHECK`'i varsayılan olarak `USING`'den
>    **kopyalar** → çapraz-tenant `INSERT` forge edilebilir. İkisi de yalnızca
>    **dikkatle** önlenebilir; §4.5 "tek bir hata sızıntıya dönmemeli" **yapısal**
>    çevreleme ister.
>
> **Sonuç — mekanizma artık somut:** çözümleme, M1-04'te uygulanan `tappa_resolver`
> (NOLOGIN, BYPASSRLS, NOSUPERUSER, default privilege YOK, yalnız `sessions`/`tags`
> tablolarına **sütun-düzeyi** `SELECT`) sahipli bir `SECURITY DEFINER` fonksiyondan
> geçer (`resolve_session_by_token_hash`; anahtar parametreli, ≤1 satır,
> `search_path` sabit `pg_catalog, pg_temp` + tablo `public.`-nitelenmiş, `EXECUTE`
> yalnız `tappa_app`'e, PUBLIC'ten `REVOKE`). Çevreleme artık disipline değil
> **yapıya** dayanır.

## Bağlam

Tappa çok kiracılı (multi-tenant) bir SaaS: iki design partner (Kebab Factory —
9 lokasyon; Kebab Manufacturing — 5 departman) ve ileride daha fazlası **aynı**
veritabanını paylaşır. [CLAUDE.md](../../CLAUDE.md) §4.5 kırmızı çizgisi tenant
izolasyonunu **her katmanda** zorunlu kılar: her tabloda `tenant_id`, her tabloda
RLS politikası, uygulamanın RLS'e tabi bir rolle bağlanması ve sorgularda ayrıca
açık `tenant_id` filtresi ("kuşak + kemer").

Bundan sonraki her migration ([M1-02](../plan/m1-veri-katmani.md)…M1-06, M1-11) ve
her sorgu ([M1-08](../plan/m1-veri-katmani.md)) bu karara uyacak. Sıfır hafızalı
bir ajan yazılı karar yoksa kendi yorumunu üretir ve izolasyon **sessizce**
delinir. Bu ADR o kararı normatif olarak sabitler.

Kararın üç girdisi M0-03 uygulamasında **canlı Postgres üzerinde ölçüldü**,
varsayılmadı (ayrıntı: Gerekçe → normatif ölçümler):

1. `app.tenant_id` GUC'una bir kez **yazıldıktan** sonra bağlantıda asla `NULL`'a
   dönmez, `''` kalır (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de) →
   [Q27](../plan/open-questions.md).
2. `tappa_owner`, docker-compose'un `POSTGRES_USER`'ı olarak initdb'nin
   **bootstrap superuser'ıdır** (`rolsuper=t`); superuser RLS'i koşulsuz atlar.
3. `FORCE ROW LEVEL SECURITY` **tablo sahibini** politikaya tabi kılar ama
   **superuser'ı** bağlamaz.

Ayrı bir yapısal kısıt da kararı şekillendirir: bir tap geldiğinde elde yalnız
tag UID (URL'den) ve oturum çerezi vardır; **ikisi de tenant taşımaz.**
`app.tenant_id` tam da bu iki aramanın *sonucudur*. Yani "her erişim tenant
bağlamında koşar" kuralı, bağlamı **kuran** aramalar için sağlanamaz — bu ADR'nin
çözmesi gereken çelişki budur (Karar madde 7).

## Karar

**1. Rol ayrımı.** Uygulama `tappa_app` rolüyle bağlanır: `NOSUPERUSER`,
`NOBYPASSRLS`, tablo sahibi **değil**. Migration'lar ayrı `tappa_owner` rolüyle
koşar (`DATABASE_MIGRATE_URL`). `internal/config` iki ayrı bağlantı dizesi ister
ve onları eşitlemeyi reddeder; ayrım config tarafından **zaten** zorlanır.
`tappa_app`'e `BYPASSRLS` **asla** verilmez.

**2. Bağlam transaction başına kurulur.** Tenant bağlamı her transaction'ın
başında `set_config('app.tenant_id', $1, true)` ile kurulur. Üçüncü argüman
`true` = transaction-local; bağlam transaction dışına **sızmaz**. `SET LOCAL`
parametre bağlama (`$1`) kabul etmediği için `set_config(...)` kullanılır; tenant
değeri **asla** string birleştirmeyle SQL'e gömülmez. Havuzdan alınan bağlantıda
`LOCAL`'siz `SET` **yasaktır**: bağlantı havuza kirli döner ve bir sonraki tenant
onun bağlamıyla sorgu koşar.

**3. Politika ifadesi — normatif ve birebir.** Her tenant politikası şu ifadeyi
yazar:

```
NULLIF(current_setting('app.tenant_id', true), '')::uuid
```

- `tenants` tablosunda: `id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`
- Diğer tüm tablolarda: `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`

`current_setting`'in ikinci argümanı `true`: ayar yoksa hata değil `NULL` döner.
`NULLIF(..., '')` ise GUC'un `''`'e düşmüş hâlini de `NULL`'a çevirir. Sonuç:
bağlam yokken ifade **her zaman** `NULL` üretir, hiçbir satır eşleşmez →
**fail-closed** (ne satır sızar ne sessiz onay — §4.6 korunur). Bu davranış
[M1-09](../plan/m1-veri-katmani.md) vaka 3'te test edilir. Çıplak biçim
(`current_setting('app.tenant_id', true)::uuid`) **terk edildi** (gerekçe:
Gerekçe → Q27 tablosu ve Değerlendirilen alternatifler).

**4. Kuşak + kemer: sorgularda ayrıca açık filtre.** Üretim sorgularının hepsi
RLS'e **ek olarak** açık `tenant_id = @tenant_id` filtresi taşır. RLS'in
kurulmadığı bir kod yolunda (unutulan `FORCE`, yanlış rol, ileride eklenen bir
tablo) tek savunma budur. **Tek istisna** madde 7'deki tenant çözümleme yoludur.

**5. `tenants` tablosunun kendisi de korunur.** `tenants`, `id` üzerinden aynı
politikayı alır (madde 3, `tenant_id` yerine `id`). Politikasız bir `tenants`
tablosu müşteri isimlerini ve VAT numaralarını sızdırır.

**6. Süper-admin / çapraz-tenant erişim MVP'de yoktur.** Böyle bir ihtiyaç
doğarsa ayrı bir rol ve ayrı bir ADR ile gelir. `tappa_app` bu amaçla asla
ayrıcalıklandırılmaz; ona `BYPASSRLS` verilmez.

**7. Tenant çözümleme istisnası — bu ADR'nin en kritik maddesi.** Yalnızca iki
sorgu — `GetTagByUID` ve `GetEmployeeBySessionHash` — tenant bağlamı kurulmadan
koşar, çünkü bağlam onların **sonucudur**. Bu istisna dar, adlandırılmış ve
testlidir:

- **Ayrı ve görünür.** İki sorgu `db/queries/resolve.sql` dosyasında durur
  (üretim sorgularının geri kalanından ayrı), böylece §4.5'in "her sorguda açık
  `tenant_id` filtresi" kuralının **tek istisnası** koda bakıldığında görünür
  kalır.
- **`tappa_app` rolüyle, `tenant_id` filtresi olmadan.** Arama küresel olarak
  tekil bir anahtara dayanır (`tags.uid` birincil anahtar; `sessions.token_hash`
  UNIQUE), dolayısıyla tenant bilinmeden de en çok **bir** satır döner.
- **Neden saf RLS değil — gerekçe "ifade edilemezlik" DEĞİL, YAPISAL çevrelemedir.**
  *(Bu maddenin önceki hâli — "saf RLS bunu tek başına ifade edemez, bypass
  kaçınılmazdır" — canlı Postgres sondasıyla çürütüldü ve düzeltildi; bkz. baştaki
  ADR güncellemesi ve Değerlendirilen alternatifler.)* Bir resolve-GUC'a
  anahtarlanmış RLS dalı çözümlemenin mutlu-yolunu **bypass'sız** ifade edebilir:
  `tappa_app` tam RLS'e tabi kalır, dönüş tek anahtarla sınırlıdır, genel bypass
  yoktur. Dolayısıyla çözümleme bir bypass'ı **kaçınılmaz kılmaz.** Alternatif yine
  de reddedildi çünkü çevrelemesi **disipline** dayanır, yapıya değil: (a) `SET
  LOCAL` unutulan tek bir kod yolunda resolve-GUC havuz bağlantısında **kalır** ve
  toplamsal OR-dalı o bağlantıdaki *her* meşru sorguya çapraz-tenant satır ekler
  (`NULLIF` fail-closed bunu yakalayamaz; `WithTenant` `app.tenant_id`'yi ezse de
  resolve-GUC'a dokunmaz); (b) `FOR ALL USING(...)` kısayolu `WITH CHECK`'i
  sessizce `USING`'den kopyalayıp çapraz-tenant `INSERT` forge'una izin verir. §4.5
  "tek bir hatanın sızıntıya dönmemesi"ni ister; bu yüzden çevreleme **yapıya**
  dayanmalı. Seçilen yapıda: bağlamı bilinmeyen bir satırı §6 FORCE altında okumak
  zaten bir `BYPASSRLS` gerektirir (RLS `USING`/`WITH CHECK` satır bazlı bir
  **boolean**'dır ve sorgunun `WHERE`'ini göremez; "yalnız anahtar aramasına izin
  ver, `SELECT *`'a verme" sınırı RLS'in kendisiyle değil aşağıdaki dar arayüzle
  konur). Karar bu bypass'ı **yok etmek değil, çevrelemektir** — en-az-ayrıcalıklı
  bir role ve tek bir `SECURITY DEFINER` fonksiyona; genel bir bypass (superuser /
  geniş `BYPASSRLS` / naif "bağlam `NULL` iken tüm satırlar" dalı) açmadan.
- **Güvenlik RLS'ten değil ARAYÜZDEN gelir.** Bağlam yokken çapraz-tenant okuma
  yalnızca dar, anahtar odaklı bir arayüzün ardında yapılır ve şu özellikleri
  **normatif** olarak taşır: (i) girdi yalnız **anahtar**dır (`uid` /
  `token_hash`); (ii) dönüş en çok **bir** satırdır (anahtarlar tekildir); (iii)
  **`SELECT *` yüzeyi yoktur** — sabit, yalnız gerekli sütunları içeren liste;
  (iv) çağıran `tappa_app`'in bu iki tabloda doğrudan çapraz-tenant `SELECT`
  hakkı **yoktur**, yalnız bu dar arayüz üzerinde `EXECUTE` hakkı vardır; (v)
  "bağlam `NULL` iken satırı göster" biçiminde naif bir RLS izin dalı
  **yasaktır** — o, `SELECT * FROM tags`'i tüm tenant'lara açan yukarıdaki (c)
  şıkkıdır. Tek satır dönmesi bir *gövde özelliği*dir; yapısal garanti bu beş
  kısıttan gelir, gövdenin iyi niyetinden değil.
- **En-az-ayrıcalık: definer superuser OLAMAZ.** Arayüz bir `SECURITY DEFINER`
  fonksiyonla kurulacaksa sahibi **superuser `tappa_owner` DEĞİLDİR.** Gerekçe:
  `SECURITY DEFINER` gövdesi **sahibinin** yetkisiyle koşar; sahip superuser ise
  gövde RLS'i **tümüyle** atlar ve arayüz kusurlu olduğunda patlama yarıçapı
  **tüm veritabanıdır** — bu tam da yasaklanan genel bypass'tır (bkz.
  Değerlendirilen alternatifler). Doğrusu — ve §6'yla birlikte okunmalı: **§6 her
  tabloya `FORCE ROW LEVEL SECURITY` zorunlu kıldığı için** bağlam yokken salt-
  `SELECT` yetkisi **yetersizdir** — RLS'e tabi bir rol 0 satır görür; okumak bir
  bypass ister. §6 FORCE altında **tek uyumlu bypass `BYPASSRLS`**'tir (FORCE'suz-
  sahip yolu §6 altında kapalı; superuser yasak). Yani definer'ın çevrelenmiş
  bypass'ı, `BYPASSRLS` olan ama yalnız bu iki tabloya (gerekli sütunlarda)
  `SELECT` GRANT'i verilmiş, adanmış ve **en-az-ayrıcalıklı** bir roldür; patlama
  yarıçapı o role verilen GRANT'larla bu iki tabloya sınırlanır, tüm DB değil.
  Böylece M1-04/M1-05 uygulayıcısı bypass'sız salt-`SELECT` bir rol kurup 0
  satırla şaşırmaz. Kesin rol adı ve DDL [M1-04](../plan/m1-veri-katmani.md)
  (sessions) ve [M1-05](../plan/m1-veri-katmani.md) (tags) migration'larında
  sabitlenir, sınırı [M1-09](../plan/m1-veri-katmani.md)'da test edilir; bu ADR
  yalnız **sınırı ve reddedilenleri** normatif koyar.
- **Çözümlemeden sonra bağlam kurulur.** İki arama tamamlanınca `app.tenant_id`
  ayarlanır ve **geri kalan her şey** `WithTenant` içinde koşar (madde 2).
- **`sys:tenant-mismatch` guardrail'i.** `token_hash` küresel UNIQUE olduğu için
  arama farklı tenant'ların Tag'i ve Session'ıyla başarılı olabilir
  (`Employee{KM}` + `Tag{KF}`). Etiketin tenant'ı ile oturumun tenant'ı
  ayrıştığında `sys:tenant-mismatch` guardrail'i
  ([M3-05](../plan/m3-policy-motoru.md), sıra 1) akışı keser.

Bu istisna yazılmazsa uygulama ya ayrıcalıklı bir rol ekler (madde 1/6 fiilen
delinir) ya da tap akışı hiç çalışmaz; ikinci hâlde geliştirici tenant
izolasyonundaki tek deliği **tam da kimlik doğrulama yolunda** açmaya itilir.

## Gerekçe

### Neden RLS'e ek olarak uygulama filtresi (kuşak + kemer)

RLS tek başına güçlüdür ama tek noktalıdır: bir tabloda `FORCE` unutulursa,
`tappa_app` yanlışlıkla ayrıcalıklandırılırsa ya da yeni bir tablo politikasız
doğarsa izolasyon **sessizce** açılır. Açık `tenant_id` filtresi bu durumda
ikinci bir bariyerdir. Tersi de doğrudur: bir sorguda `WHERE tenant_id` unutulsa
RLS yakalar. İkisi birlikte, tek bir hatanın çapraz-tenant sızıntıya dönmesini
engeller. Bu yüzden filtre RLS'in **yerine** değil, **yanına** konur.

### Q27 — neden `NULLIF`, neden çıplak cast değil (normatif ölçüm)

`tappa_app` ile canlı sonda:

| bağlantının durumu | `current_setting('app.tenant_id', true)` | çıplak `::uuid` | `NULLIF(...)::uuid` |
|---|---|---|---|
| GUC'a **hiç yazılmamış** | `NULL` | `NULL` → 0 satır | `NULL` → 0 satır |
| bir kez **yazılmış**, tx bitmiş | `''` | **`ERROR: invalid input syntax for type uuid: ""`** | `NULL` → 0 satır |

Tetikleyici bağlantının kaçıncı kullanımı değil, GUC'a **ilk yazma**dır: taze bir
bağlantıda arka arkaya üç bağlamsız sorgu üçünde de `NULL` / 0 satır / hatasız
geçti. `''`'e düştükten sonra `ROLLBACK`, `RESET app.tenant_id` ve `DISCARD ALL`
de `NULL`'a döndürmez (üçü ayrı ayrı ölçüldü); tek yol **yeni bağlantı**dır.
`pgxpool` altında bu, ilk `SET LOCAL`'dan sonra o bağlantı için **kalıcı**
durumdur.

İki biçim de **fail-closed**; kırılan şey güvenlik değil **determinizm**: aynı
hata (unutulmuş bağlam), bağlantı geçmişine göre iki farklı davranış üretir —
taze bağlantıya karşı yazılmış bir test geçer, üretim patlar. `NULLIF` her iki
durumda da `NULL` ürettiği için seçildi; uçtan uca doğrulandı (aynı bağlantıda
tenant A → 1 satır, commit sonrası bağlamsız → 0 satır, tenant B → 1 satır, hata
yok).

**Bedeli açıkça:** `NULLIF` unutulan bağlamı **sessizce** 0 satıra çevirir;
çıplak biçimin gürültülü hatası bir bug'ı daha çabuk yakalardı. Bu bedel giriş
noktasında telafi edilir: [M1-07](../plan/m1-veri-katmani.md) `WithTenant`
sarmalayıcısı `set_config('app.tenant_id', $1, true)`'i **kendisi** kurar ve
bağlam kurulmadan sorgu çalıştırmayı **API olarak imkânsız** kılar. Yani koruma
politikadan değil giriş noktasından gelir.

### M0-03 — `tappa_owner` superuser'dır; `FORCE` onu bağlamaz (normatif ölçüm)

`FORCE ROW LEVEL SECURITY`'nin kendisi **çalışır**: geçici yaratılan
**NOSUPERUSER** bir tablo sahibi (`rolsuper=f`, `rolbypassrls=f`), `ENABLE`+`FORCE`
RLS'li kendi tablosunu bağlam kurmadan okuduğunda **0 satır** gördü — yani `FORCE`
salt tablo sahipliğini yener. Ama `tappa_owner` initdb'nin bootstrap
superuser'ıdır ve **superuser RLS'i koşulsuz atlar**; `FORCE` ona erişemez.

| rol / koşul | RLS'e tabi mi? |
|---|---|
| superuser (`tappa_owner`) | **Hayır** — koşulsuz atlar, `FORCE` erişemez |
| `BYPASSRLS` rolü | **Hayır** |
| tablo sahibi, `FORCE` **yok** | **Hayır** (Postgres varsayılanı) |
| tablo sahibi, `FORCE` **var** | **Evet** — `FORCE` sahipliği yener |
| `tappa_app` (NOSUPERUSER, NOBYPASSRLS, sahip değil) | **Evet** |

**Normatif sonuç:** CLAUDE.md §8'in zorunlu kıldığı RLS izolasyon testi
`tappa_app` ile — yani `DATABASE_URL` havuzuyla — koşmak **zorundadır**.
`tappa_owner` ile koşan bir izolasyon testi RLS'i **hiç** sınamaz; üstelik
sessizce değil **gürültülü** geçer (owner satırları görür, testin negatif
assertion'ı patlar) — tehlike "yanlış güven" değil, bozukluğun yanlış yerde
(RLS'te) aranmasıdır; oysa roldedir. Bu yüzden M1-09 testinin içinde
`SELECT current_user` = `tappa_app` **ve** `rolsuper/rolbypassrls = f,f` açıkça
doğrulanır — havuzu "appPool" diye **adlandırmak** kanıt sayılmaz.

### İzolasyon testi ≠ üretim sorgusu (normatif ayrım)

§4.5 **üretim** sorgularının RLS'e ek olarak açık `tenant_id` filtresi taşımasını
zorunlu kılar. Ama **izolasyon testi** bu filtreyi taşımamalıdır: taşırsa 0
satırın sebebi RLS değil `WHERE`'dir ve test RLS kapalıyken de yeşil kalır. Aynı
iddia ("bağlam B iken A'nın `id=1` satırı okunamaz, 0 beklenir") iki sorgu
şeklinde ölçüldü:

| sorgu şekli | `tappa_app` | `tappa_owner` | vaka ne ölçer |
|---|---|---|---|
| ham: `ctx=B`, `WHERE id=1` | **0** ✅ | 1 ❌ | RLS gerçekten sınanır |
| §4.5 store biçimi: `ctx=B`, `WHERE id=1 AND tenant_id=<B>` | **0** ✅ | **0** ✅ | iki rolde de geçer — RLS sınanmaz |

İkisi çelişki değil, **farklı iştir**: üretim sorgusu filtreyi taşımak zorundadır
(§4.5), izolasyon testi ise RLS'in yalın işini ölçmek zorundadır (§8). Filtreli
biçim ayrı bir vaka olarak durabilir ama izolasyon kanıtı **sayılmaz**
(M1-09'da `TestStoreQueryFiltersByTenant` gibi ayrı adlandırılır).

## Sonuçlar

- **[M1-02](../plan/m1-veri-katmani.md)…M1-06, M1-11:** her tenant politikası
  Karar madde 3'teki `NULLIF(...)` biçimini **birebir** yazar (`tenants` `id`
  üzerinden, diğerleri `tenant_id` üzerinden). Her tablo CLAUDE.md §6 beşlisiyle
  doğar: `tenant_id NOT NULL` · `tenant_id` indeksi · `ENABLE`+`FORCE ROW LEVEL
  SECURITY` · hem `USING` hem `WITH CHECK` politikası · `tappa_app` GRANT.
  `scripts/redline-check.sh` R5 bunu tarar (tarama geçmesi ihlal olmadığını
  kanıtlamaz — §4).
- **[M1-04](../plan/m1-veri-katmani.md) (sessions), M1-05 (tags):** Karar madde 7
  için bağlamsız, **anahtar-kısıtlı** erişim kurulur; genel bypass açılmaz.
- **[M1-07](../plan/m1-veri-katmani.md):** `WithTenant`
  `set_config('app.tenant_id', $1, true)`'i kendisi kurar; `LOCAL`'siz `SET`
  hiçbir yerde geçmez. Bu, `NULLIF`'in "sessiz 0 satır" bedelinin telafisidir.
- **[M1-08](../plan/m1-veri-katmani.md):** `GetTagByUID` ve
  `GetEmployeeBySessionHash` `db/queries/resolve.sql`'de durur; kalan tüm sorgular
  açık `tenant_id = @tenant_id` filtresi taşır.
- **[M1-09](../plan/m1-veri-katmani.md):** izolasyon vakaları (1, 2, 3, 7)
  `tappa_app`/`DATABASE_URL` havuzuyla ve **ham** (filtresiz) sorguyla koşar;
  vaka 5 (`tappa_owner` `DELETE` → trigger) owner havuzuyla; her negatif vakaya
  pozitif kontrol eşlik eder (koruma kapatılınca kırmızıya döner).
- **[M3-05](../plan/m3-policy-motoru.md):** `sys:tenant-mismatch` guardrail'i
  (sıra 1) çözümleme sonrası tenant ayrışmasını keser.
- **[CLAUDE.md](../../CLAUDE.md) §6** bu ADR ile tutarlıdır: politika ifadesi
  (`NULLIF`) ve "izolasyon testi ile üretim sorgusu farklı şekiller ister"
  ayrımı orada zaten güncel.
- Bu ADR yazılmadan hiçbir M1 migration'ı başlamamalıydı; artık M1-02 buna
  referansla yazılabilir.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| **Şema-per-tenant** (her tenant ayrı Postgres şeması) | Ölçekte şema/migration çoğaltması operasyon yükü; çapraz-tenant çözümleme (madde 7) ve tek `transactions` raporlaması zorlaşır; tek VPS'te şema/bağlantı patlaması. RLS tek şemada aynı izolasyonu daha ucuz verir. |
| **Yalnız uygulama filtresi** (RLS yok, her sorguda `WHERE tenant_id=`) | Tek bir unutulan `WHERE` sessiz çapraz-tenant sızıntısıdır; §4.5 izolasyonu "her katmanda" ister. Filtre kalır ama RLS'in **yanına** (kuşak+kemer), tek savunma olarak değil. |
| **Çıplak cast** `current_setting('app.tenant_id', true)::uuid` | Fail-closed ama **belirsiz**: GUC'a ilk yazmadan sonra bağlamsız sorgu `0 satır` değil `ERROR` verir (Q27 tablosu). Davranış bağlantı geçmişine bağlı; taze bağlantıya yazılmış test geçer, üretim patlar. `NULLIF` determinizm için seçildi. Çıplağın tek üstünlüğü (gürültülü patlama) M1-07 `WithTenant` ile telafi edildi. |
| **`tappa_app`'e `BYPASSRLS`** (çözümlemeyi kolaylaştırmak için) | Tüm izolasyonu tek satırda iptal eder; Karar madde 6/7 bunu açıkça yasaklar. Çözümleme dar, anahtar-kısıtlı istisnayla halledilir. |
| **Superuser `tappa_owner`'a ait `SECURITY DEFINER` çözümleme fonksiyonu** | `SECURITY DEFINER` gövdesi sahibinin yetkisiyle koşar; sahip superuser olduğu için gövde RLS'i **tümüyle** atlar → tüm veritabanını kapsayan genel bir bypass, patlama yarıçapı sınırsız. Tek satır dönmesi bir *gövde özelliği*dir, ayrıcalık modelinin yapısal garantisi değil. Çözümleme, en-az-ayrıcalıklı (yalnız iki tabloya erişen) bir arayüzün ardında yapılır (Karar madde 7). |
| **GUC-anahtar saf-RLS** (`app.resolve_token` GUC'una anahtarlanmış, toplamsal bir RLS dalı; **bypass yok**) | Mutlu-yolu **bypass'sız** ifade eder ve "saf RLS bunu ifade edemez" iddiasını çürütür (canlıda doğrulandı: `tappa_app` tam RLS'e tabi, dönüş tek anahtarla sınırlı). Reddedilme sebebi ifade gücü değil, çevrelemenin **yapısal olmaması**: iki tek-nokta hatası canlıda çapraz-tenant ihlali üretti — (1) **okuma:** `SET LOCAL`'sız kurulan resolve-GUC havuz bağlantısında kalır, OR-dalı o bağlantıdaki her meşru sorguya bir çapraz-tenant satır sızdırır (`NULLIF` fail-closed yakalayamaz, `WithTenant` resolve-GUC'a dokunmaz); (2) **yazma:** `FOR ALL USING(...)` kısayolu `WITH CHECK`'i varsayılan olarak `USING`'den kopyalar → çapraz-tenant `INSERT` forge. İkisi de yalnız **disiplinle** engellenir; §4.5 yapısal çevreleme ister. Bu yüzden en-az-ayrıcalıklı `tappa_resolver` + anahtar-kısıtlı `SECURITY DEFINER` fonksiyon seçildi (Karar madde 7). |
| **RLS testini `tappa_owner` ile koşmak** (tek havuz) | `tappa_owner` superuser'dır ve RLS'i koşulsuz atlar (M0-03 tablosu); testin negatif vakaları gürültülü patlar. İzolasyon `tappa_app` ile ölçülmek **zorundadır**. |
