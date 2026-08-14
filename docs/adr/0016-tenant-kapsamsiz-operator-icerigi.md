# ADR 0016 — Tappa'nın kendi yasal metinleri tenant kapsamsız bir tabloda durur

- **Durum:** kabul edildi
- **Tarih:** 2026-08-14
- **Bağlam:** M7-06 (yasal metin formu), migration
  [00020](../../db/migrations/00020_create_legal_documents.sql)
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) (tenant bağlamı, çözümleyici
  muafiyetleri) · CLAUDE.md §4.5, §6 · `scripts/redline-check.sh` R5 muafiyeti

## Neden ayrı bir ADR

CLAUDE.md §6 açık: *"Her yeni tablo şunlarla doğar: `tenant_id uuid NOT NULL`, …"*.
`legal_documents` bu beşliyi **karşılamıyor** ve karşılayamaz. Kural sessizce
esnetilemez; §6'nın kendisi de değiştirilemez, çünkü kural **tenant verisi** için
doğru. Bu yüzden istisna gerekçesiyle, ölçümüyle ve **elenen şıkkın fiyatıyla**
yazılıyor.

## Bağlam

M7-01 dört yasal belgeyi (**gizlilik politikası · hizmet şartları · şirket künyesi ·
çerez bilgilendirmesi**) **iskelet** olarak sevk etti: her sayfa ne içereceğini
söylüyor, *"Nothing on this page is in force"* diyor ve beklediği olguları madde
madde basıyor. Üç oturum boyunca olgular gelmedi — sebebi unutkanlık değil
**yapıydı**: metinleri girmenin tek yolu Go kaynağını düzenlemekti. Kullanıcı
(2026-08-14): *"benden beklenenleri admin panelden girilebilir yap … süper admin
girsin güncelleyebilsin, bu kadar basit."*

Bu metinler **hiçbir tenant'a ait değil**. Her ziyaretçi aynı metni okur ve onları
basan sayfanın **hiçbir kimliği yoktur** (`handler.Marketing`: havuz yok, çerez yok,
audit yok).

## Karar

### 1. Tablo `tenant_id` TAŞIMAZ, ve muafiyet gürültülü

`legal_documents` sütunları: `id · slug · body · published_at · published_by`.
`tenant_id` yok. Muafiyet migration'a `scripts/redline-check.sh`'in zaten anladığı
sözdizimiyle yazıldı:

```
-- redline: no-tenant-scope(legal_documents) — …
```

Bu, o mekanizmanın **repodaki ilk kullanımı**; script yazıldığından beri muaf edecek
bir şey yoktu. Tarama her koşuda **WARN** basıyor (ölçüldü: `./scripts/redline-check.sh`
→ `[R5 · WARN] Tenant kapsamindan MUAF birakilmis tablo(lar)`, exit 0). Muafiyet
görünmez olamaz.

⚠️ **Muafiyet beşli denetimin TAMAMINI atlar.** Bu yüzden RLS (`ENABLE` + `FORCE` +
`USING (true)` politikası) **gönüllü** olarak yazıldı ve canlı katalogda test
edilerek tutuluyor (`TestLegalDB_TheTableIsVisibleToEveryTenantAndScopedToNone`),
script tarafından değil.

### 2. ELENEN ŞIK: "Tappa'nın kendi tenant'ı" — ve eleyen ölçüm §4.5

İkinci okuma şuydu: içeriği **Tappa'nın kendi tenant'ına** yazmak. Sıfır §6
istisnası. **Eleyen sebep bir tercih değil, bir yetenek:**

Operatörün admin hesabı bir **müşteri tenant'ında** yaşıyor (kullanıcının kendi
hesabı `admin_users.role ∈ ('owner','manager')`, ikisi de tenant kapsamlı — 00006:66).
İçerik Tappa'nın tenant'ında olsaydı, yazma yolu şu şekli almak zorundaydı:

```go
// müşteri tenant'ındaki bir oturumla ulaşılabilen bir handler:
db.WithTenant(ctx, tappaTenantID /* çağıranınki DEĞİL */, …)
```

Bu **tam olarak** §4.5'in var olma sebebi olan çapraz-tenant yeteneğidir ve görevin
kırmızı çizgisi *"bu yetenek hiç doğmamalı"* diyordu. Kaçınmanın tek yolu operatöre
Tappa tenant'ında **ikinci bir hesap** (ve ikinci bir giriş) vermekti — kullanıcının
*"bu kadar basit"* talebinin ve orkestratörün *"yeni giriş yok"* kararının reddettiği
şey.

**Seçilen şıkta çağıran hiçbir zaman kendi tenant'ından başkasını adlandırmıyor:**
`Store.Publish` `WithTenant(callerTenant, …)` ile açıyor, belge satırı tenant
istemiyor, `audit_log` satırı **çağıranın kendi** tenant'ına yazılıyor. Yapısal
kanıt üretilen tiplerin üstünde: `store.PublishLegalDocumentParams`'ta `TenantID`
alanı **yok**, `ListPublishedLegalDocuments` yalnız `ctx` alıyor
(`TestLegalStore_CannotNameATenantAtAll`, bağımsız pozitif kontrolüyle —
`RecordAuditEventParams` **taşımak zorunda**).

**Elenen şıkkın diğer fiyatları (sayıldı):** herkese açık `/legal/*` yolu o tenant'ın
id'sini **config'den** okumak zorunda kalırdı (yanlış yapılandırma = yanlış metin);
tenant `tenants` tablosunda görünüp faturalamada ve `resolve_admin_by_email`
penceresinde **bir müşteri gibi** davranırdı.

### 2b. Ve istisnanın **gerçek fiyatı**: yazma tarafında **DB derinliği yok**

Bu, 2. turda ölçülüp eklendi çünkü ilk sürüm bunu yazmıyordu ve migration'ın yorumu
tersini ima ediyordu. Ölçüldü (`BEGIN … ROLLBACK`, **yabancı** bir tenant bağlamında,
`tappa_app` olarak): `INSERT INTO legal_documents` **başarılı** (50 → 51), aynı
bağlamda `admin_users` ve `tenants` **0 satır**.

**Yani ürünün her yerindeki kuşak+kemer (§4.5: RLS + açık `tenant_id` filtresi) bu
tabloda YOK ve OLAMAZ** — ayıracak bir tenant sütunu olmadığı için GRANT da politika
da kimseyi kimseden ayırmaz. *"Kim yazabilir"* sorusunun **tek** cevabı uygulama
katmanındaki izin listesidir (`internal/handler.mayPublishLegal`). Bu, tenant kapsamsız
bir tablonun **kaçınılmaz** sonucudur ve muafiyetin ödediği bedeldir; karşılığında
alınan şey 2. maddedeki yetenek: **çapraz-tenant yazma hiç doğmuyor.**

**Ayakta kalan iki koruma:** *append-only* (yanlış yazılan bir satır **gizlenemez**,
§4.3) ve `slug` CHECK'i (yazılabilecek belge kümesi kapalı).

⚠️ **İlgili, kapatılmadı:** izin listesinin anahtarı `admin_users.id` ve `tappa_app`
**ayrıcalık düzeyinde** hâlâ seçtiği id ile admin ekleyebilir. Bugün kapalı olan şey
**sorgu metni** (üretimdeki tek INSERT `id`'yi adlandırmıyor), ayrıcalık değil.
Kapatmanın fiyatı **~20 test fixture'ı**; kazancı ise **artımlı olarak sıfıra yakın**,
çünkü onu sömürebilen saldırgan zaten yukarıdaki ölçüme göre `legal_documents`'a
doğrudan yazabilir. M9-08'e limit olarak yazıldı.

### 3. Tablo APPEND-ONLY (§4.3 ailesi)

`GRANT SELECT, INSERT` · `REVOKE UPDATE, DELETE` · 0005'in
`tappa_forbid_mutation()` trigger'ı (tablo sahibini de bağlar). Düzeltme = **yeni
satır**; `transactions`, `audit_log`, `policy_versions`, `billing_periods` ile aynı
şekil. Gerekçe: *"şikâyet gününde politika ne diyordu"* bu tabloya sorulacak tek
sorudur.

**Ölçüldü (mutasyon):** REVOKE + trigger migration kaynağından kaldırılıp yeniden
uygulandığında `TestLegalDB_TheTableTakesNoUpdateAndNoDelete` üç sondada birden
kırmızıya dönüyor (`UPDATE the body` · `UPDATE the date` · `DELETE the row`).

### 4. `published_by`'da FK YOK

`admin_users`'a bir FK, FK denetiminin RLS'i görmemesi sayesinde *"bu uuid var mı"*
sorusunu cevaplayan bir **çapraz-tenant varlık kehaneti** olurdu (tahmin edilen id
ile INSERT → `23503` ya da başarı). `audit_log.actor_id` ile aynı gerekçe (00005).

### 5. Kim yazabilir: rol DEĞİL, **env izin listesi** — ve anahtar **e-posta değil
`admin_users.id`**

Gerekçeler:

- **Yeni rol yok.** `admin_users.role` kapalı sözlüğünü **policy motoru** okuyor
  (`actor:role`, `internal/domain/tenant/rulewriter.go:250`); tanımadığı bir rolün ne
  yapacağı ayrı ve büyük bir iştir.
- **Yeni giriş yok.** Ayrı bir operatör girişi = ikinci çerez, ikinci çözümleyici,
  ikinci kilitlenme hikâyesi. Dört belge bunu haklı çıkarmaz.
- **Fail-closed.** Boş liste **hiç kimseyi** kabul eder — `if len(allow) == 0 {
  return true }` dalı hiçbir yerde yok. Mutasyonla doğrulandı: o dal eklendiğinde
  `TestOperatorGate_EmptyAllowListAdmitsNobody` 200/303 görüp kırmızıya dönüyor.

🔴 **ANAHTAR ÖNCE E-POSTAYDI VE BİR GÜVENLİK DENETİMİ ONU UÇTAN UCA KIRDI.** Bu, bu
ADR'nin en önemli maddesi, çünkü kırılma açık değil:

- `admin_users` e-posta tekilliği **tenant başınadır**
  (`admin_users_tenant_email_key UNIQUE (tenant_id, email)`), **global değil.**
- `/signup` **herkese açıktır** ve adresi **çağıran yazar**; repoda **hiçbir e-posta
  doğrulaması yok** (`internal/domain/signup` yalnız uzunluk + tek `@` şekli bakıyor;
  VIES başarısızlığı kaydı durdurmuyor).
- **Ölçüldü:** izin listesindeki adresi taşıyan **yabancı bir tenant**, rolü
  *manager*, `GET /admin/legal` → **200**, `POST` → **303**, ve yayımlanmış gizlilik
  politikası değiştirildi.

**Genel ifade: bir izin listesi ancak ANAHTARININ TEKİLLİĞİ kadar değerlidir**, ve bu
şemada bir e-posta adresinin tekilliği yoktur. Ayrıca ikincil bir üyelik kehaneti
vardı (200 ↔ 403, saldırganın kendi kontrol ettiği bir adresle kaydolarak
okunabilir).

**Sevk edilen anahtar `admin_users.id`:** tablonun **PRIMARY KEY**'i, **global
tekil**, ve değerini **veritabanı** atıyor (`gen_random_uuid()`);
`internal/domain/signup` onu INSERT'ten **geri okuyor**, seçmiyor. Hiçbir form,
başlık ya da kayıt akışı onu **beyan edemez** — eski anahtarın tam olarak
eksik olduğu özellik bu. Regresyon: `TestOperatorGate_IsNotJoinableByRegisteringABusiness`.

**Ve rekey bir yan fayda getirdi:** `AdminUserID` çözümlenen kimlikte **zaten
vardı**, dolayısıyla ilk sürümün `TouchAdminSession`'a e-posta eklemesi **tamamen geri
alındı** — kimlik doğrulama sıcak yolu, `adminauth.Resolved` ve `db/queries/admins.sql`
bu görevden **hiç etkilenmiyor**, ve panelde kişisel veri genişlemesi olmadı.

**Ergonomi kapatıldı, kullanıcıya uuid aratılmıyor:** `/admin/legal`'in reddetme
sayfası **çağıranın KENDİ** admin id'sini basıyor ve *"TAPPA_OPERATOR_ADMIN_IDS bunu
alır"* diyor. Bir admin id'si sır değildir (kendi satırının PK'si, o hesabın sebep
olduğu her `audit_log` satırında var, parolanın yerine geçmez); sayfa izin listesini,
boş olup olmadığını ya da başka bir admin'i **asla** basmıyor
(`TestOperatorRefusal_ShowsTheCallerTheirOWNIdAndNobodyElses`).

### 6. Yazma yolu **sınırlı**, okuma yolu **hesaplanmış**

Denetimin ikinci bulgusu: `POST /admin/legal` **panelin gövde sınırı olmayan tek
POST'uydu** (diğer sekizi 4–16 KiB bağlıyor) ve yazdığı tablodan hiçbir şey
silinemiyor. Ölçüldü: 1/4/9 MiB gövdeler **kabul edildi ve saklandı**;
`adminSessionLimit` ile oturum başına ~2,7 GB/pencere, temizlik yolu yok.
İkinci yarısı herkese açık sayfaydı: 9 MB'lık bir gövde **anonim GET başına 253 ms**
CPU'ya mal oluyordu (`normalizeNewlines`'ın `for strings.Contains(...)` döngüsü).

**Üç düzeltme birlikte:** `http.MaxBytesReader` ile **256 KiB** (diğer sekizle aynı
desen) · paragraf bölme **yayım anında bir kez** (anlık görüntüde saklanıyor, her
istekte değil) · `normalizeNewlines` **tek geçiş**.

## Sonuçlar

- **Kabul edilen:** §6'nın beşlisi bu tabloda yok; kural **tenant verisi** için
  geçerli kalıyor ve muafiyet her `redline-check` koşusunda bağırıyor.
- **Kabul edilen:** izin listesi `.env`'de, veritabanında değil. Değiştirmek dağıtım
  gerektirir. Bedeli: dört belge için bir satır. Karşılığı: sıfır yeni kimlik yüzeyi.
- **Kabul edilen sınır:** `published_by` uygulama rolü tarafından **okunamaz**
  (00020 sütun düzeyinde INSERT verir, SELECT vermez), çünkü tenant kapsamsız bir
  tabloda okunabilir bir admin uuid'si zayıf bir çapraz-tenant varlık kehanetiydi
  (denetim ölçtü). Provenance korunuyor ama adli okuma `tappa_owner`'ın işi; ürün
  içinde *"bu metni kim yayımladı"* sorusunu cevaplayan bir ekran **yok**.
- **Kabul edilen sınır — BAYATLIK İKİ KAYNAKTAN GELİR, İKİSİ DE SAYILDI.**
  **(i) Anlık görüntü:** `Published()` bir **bellek anlık görüntüsüdür**; yayımlayan
  süreç tazeler. Tek süreçte (CLAUDE.md §1: tek VPS) doğru — denetim doğruladı:
  yayımlayan **kendi değişikliğini anında** görüyor, ikinci bir süreç **20 sn sonra da**
  eskisini. Bunu kapatan şey `Store.Refresh`'i bir zamanlayıcıya bağlamaktır ve
  **yapılmadı**.
  **(ii) HTTP önbelleği — bu, ilk sürümün SAYMADIĞI ikinci kaynak.** `/legal/*`
  `Cache-Control: public, max-age=300` ile servis ediliyor (M7-01'in kararı,
  `marketing.go`), yani **tek süreçte bile** bir ara katman ya da tarayıcı yayımdan
  sonra **5 dakikaya kadar** eski metni gösterebilir. İki kaynak **toplanır**: en kötü
  hâl bir sonraki süreç yeniden başlamasına kadar + 5 dk. Bir yasal metin için kabul
  edildi çünkü yayım **nadir** ve gecikme **kısa ve sınırlı**; kapatmanın fiyatı bu
  yüzeyin cacheable olmasından vazgeçmek ya da yayımda bir önbellek geçersizleştirme
  yolu icat etmek olurdu.
- **Kabul edilen sınır:** ürün bir yasal metnin **yeterli** olup olmadığını yargılayamaz.
  Garanti edilen şey dar: sayfa *"yayımlanmadı"* demeyi ancak biri **kasten yayımladığı
  için** bırakır.
