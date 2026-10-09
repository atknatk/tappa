# M11 — Entegrasyon API'si (KF-RMS · KM-ERP)

> **Durum:** PLAN, **2. sürüm** (2026-10-09, 28. oturum). Kod yok.
> 1. sürüm (`a169391`) iki bağımsız denetimden **RED** aldı (üçüncü göz + güvenlik
> merceği; 6 bloklayan, ~14 orta, ~20 düşük bulgu). Bu sürüm hepsini işler — hangi
> bulgunun nereye işlendiği §9'da. Kullanıcı kararları: K-1, K-2, K-3, K-19 (§3).
> Sıradaki adım: 2. tur denetim, ONAY gelirse API-0 (ADR'ler).
>
> **Kaynak:** kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve
> KM-ERP entegrasyonu)"*, 2026-10-09. **Repoya konmadı** — depo public ve doküman
> müşterinin iç webhook adresini taşıyor. Bu dosyada "spec §N" o dokümanın
> bölümüdür.

**Amaç.** Tenant'ın panelinden ürettiği **API anahtarlarıyla** dış İK/ERP
sistemlerinin (ilk müşteri: KF-RMS ve KM-ERP) Tappa'ya lokasyon ve çalışan
göndermesi, punch'ları **imleçli akıştan** çekmesi ve **webhook** ile anında
alması. ZKBio'nun yerini alan entegrasyon yüzeyi budur.

**Neden ayrı milestone.** Bugün dış dünyaya açık hiçbir JSON API, Bearer kimliği,
webhook, OpenAPI ya da Idempotency-Key yok (ölçüldü). Bu iş yeni bir **güvenlik
sınırı** (makineden makineye kimlik), yeni bir **giden ağ yüzeyi** (müşterinin
seçtiği URL'ye istek) ve yeni bir **sıralama sözleşmesi** (punch akışı) açar —
üçü de ADR ister (CLAUDE.md §10).

---

## 1. Spec ↔ Tappa — bugünkü durum (ölçüldü, iki denetimde yeniden üretildi)

| Spec beklentisi | Tappa bugün | Plan |
|---|---|---|
| İki şirket (KF/KM), **her çalışan iki şirketin her lokasyonunda okutabilir** | KF ve KM **iki ayrı tenant**; gruplama kavramı yok. Yabancı tenant plaketi → `sys:tenant-mismatch` → **403, kayıt yok** (`internal/policy/guardrails.go:306-319`, `internal/handler/checkin.go:227-233`) | **K-1** |
| Location: `externalRef`, `timezone`, `active` | external id yok · saat dilimi **tenant** sütunu · aktif bayrağı yok, silme sert `DELETE` (`db/queries/locations.sql:369`) | API-1, K-7, K-20 |
| Tag panelden lokasyona bağlanır | Var (`tags.uid` küresel PK, `location_id`, durum). QR satırları da `tag_uid` taşır (`internal/domain/checkin/checkin.go:1282-1285`) | Değişmez; `tagId = tag_<uid>` |
| Employee: `externalRef`, `employeeCode`, `firstName`, `lastName`, `email`, `active`, `activated` | external id/kod **yok** · tek `full_name` · `email citext`, tekilliği `(tenant_id, email) WHERE email IS NOT NULL` — **pasif satırlar dahil** · `status` + `activated_at` **var** · `location_id NOT NULL` (spec'te karşılığı yok) · `updated_at` yok | API-1, K-14, K-21 |
| Yeni çalışana **otomatik** aktivasyon e-postası | Çalışan eklemek davet **göndermez**. Davet e-postası **senkron**, istekte. Tavanlar: tenant 50/saat 300/gün · **çalışan 3/saat** · kutu 5/saat 20/gün · süreç kesicisi 300/saat. Prod e-posta açılışı (EM-5B) `m10-a1`'de commit'lendi (`c447c88`), canlıya çıkışı `main` birleştirmesine bağlı | API-6, K-16, K-17 |
| **İlk dokunuş = ilk punch** | ADR 0026: aktivasyon dokunuşu **`transactions` satırı yazmaz** | **K-2** |
| "Doğrulanamayan dokunuş punch'a dönüşmez" | `flag` kaydı **yazılır**, onay kuyruğuna düşer (§4.6); onay ayrı tablo `transaction_reviews` (append-only, `UNIQUE(transaction_id)`) | K-4 |
| Düzeltme/iptal → yeni `version` | Geçerli kaydı iptal yolu **yok**; düzeltme = bağsız yeni manuel satır (ADR 0011) | K-5 |
| Manuel punch'lar RMS/ERP'de (spec §9) | Tappa'da `channel='manual'` kayıt var | K-6 |
| İmleçli akış, **kayıt sırasına göre**, geç gelen kaçmaz | `transactions`'ta **hiçbir monoton anahtar yok** (`gen_random_uuid()`), `created_at` = işlem başlangıcı ≠ commit sırası | API-2 (K-13) |
| Webhook + imza + tekrar deneme | DB outbox/kuyruk **yok** (yalnız bellekte sıfırlama kuyruğu); dış HTTP yalnız VIES (sabit URL); **dial-time SSRF koruması yok, pod egress'i açık** | API-9, API-10, K-19 |
| Sistem başına API anahtarı | Yok. Token kalıbı var (32 bayt, HMAC'li hash, redakte tip), paylaşılan yardımcı yok | API-3 |
| Idempotency-Key, `limit ≤ 1000`, JSON hata gövdesi | Yok. `httpx.Limiter` (bellekte, `replicas: 1`) **ve 429'da `Retry-After` zaten var** (`internal/httpx/ratelimit.go:551`) | API-4 |
| OpenAPI + sandbox | Yok. Dokunuş simülatörü dört katlı kapının arkasında: `TAPPA_ENV=dev` **ve** loopback `BaseURL` (`internal/config/config.go:731-745`, `internal/handler/devtap.go:71-78`) | API-11, API-12, **K-3** |
| Bir telefon tek bir çalışana bağlı | **Var** — tarayıcı başına tek `tappa_session` çerezi | — |

**Eşleme notu — KM'nin "departmanları".** Spec'te KM-ERP departmanları
(`DEP-1`…) **lokasyon** olarak gönderir ve etiket onlara bağlanır. Tappa'da
plaket **lokasyona** bağlanır; departman lokasyonun alt birimidir. Spec'in
Location'ı = Tappa `locations`; KM'nin her departmanı Tappa'da **bir lokasyon**
olur. Tappa `departments` v1 API'sinde görünmez.

---

## 2. Sözleşme farkları — entegratöre bildirilecek

API-12'nin teslim ettiği rehberde bu liste **açıkça** yazılır.

1. Aktivasyon dokunuşu punch **değildir**; ilk punch ikinci dokunuştur (K-2).
2. `flag` verdict'li dokunuş yalnız **müdür onaylarsa**, **onay anında** akışa girer;
   reddedilen hiç gelmez (K-4).
3. v1'de `version` hep `1`, `voided` üretilmez (K-5). Alanlar sözleşmede durur.
4. Ek alan: `channel` (`nfc` | `qr`). Her iki kanalda da `tagId` doludur.
5. `punchTime` = Tappa'nın `occurred_at`'ı: telefonun **beyan ettiği** an. Çevrimdışı
   kuyruk 120 sn'ye kadar `ok` kalır; daha eskisi onaya düşer (2. madde); tavan 72 sa.
   `recordedAt` = sunucunun kaydı yazdığı an.
6. 60 sn içindeki tekrar dokunuş (`ignored`, §5 satır 5) punch **değildir** — spec'in
   "her dokunuş bir punch" ifadesinin tek istisnası.
7. Lokasyon `timezone`'u tenant'ın saat dilimine eşit olmalı (bugün `Europe/Malta`), değilse 422 (K-7).
8. Lokasyonda `active:false` **yalnız etikettir**: dokunuş kararını değiştirmez;
   plaketleri durdurmak için panelden emekliye ayrılırlar (K-20).
9. Yeniden işe alım (pasif → `active:true`) **yeni `employee.id`** üretir;
   `externalRef` aynı kalır (ADR 0010).
10. API ile açılan lokasyon panelden **GPS/IP** tanımlanana kadar dokunuşları onaya düşer → onaylananlar sonradan gelir.
11. Çalışan e-postası: ASCII olmalı, aynı tenant'ın bir yönetici adresi olamaz (422).
    Davet her zaman **kuyruğa** girer (`activationEmail: queued`), tavanlar
    gönderimi geciktirir ama isteği reddettirmez (K-17).
12. Aktivasyon yalnız NFC ile (QR aktive edemez; iPhone X ve öncesi aktive olamaz).
13. Aktive olmuş çalışana `resend-activation` → 409 `already_activated` (cihaz değişimi yalnız panelden).
14. Webhook'ta sıra garantisi yok; tekilleştirme `id` + `version` ile.
15. Manuel kayıtlar akışa girmez (K-6).
16. Geçmiş sorgusu (`from`/`to`) en çok 31 gün, sayfalı.
17. `updatedSince` (rehber) **10 dakikalık örtüşme penceresiyle** çalışır: dönen kayıt
    kümesi tekrar içerebilir, tüketici `id` ile ezerek yazar (K-21).
18. API ve webhook, operatörün tenant için açtığı erişimle çalışır (K-22).

---

## 3. Kararlar

### Kullanıcı kararları — ✅ 2026-10-09

| # | Karar | Seçilen | Bedeli (kayıtlı) |
|---|---|---|---|
| **K-1** | Çapraz şirket dokunuşu (§4.5'e değer) | **(A) Tek tenant, iki şirket** (`companies` boyutu). Denetimde çıkan ek bedellerle **yeniden onaylandı** | (1) KM yöneticileri birleşik tenant'ta kendi hesabını ancak **EM-12** (ek yönetici/müdür rolü) ile alır → **EM-12 M11'in canlı ön koşulu** · (2) tenant markası KM çalışanının tap/davet ekranında KF'ninki · (3) eski KM tenant'ı operatör kapatana kadar faturalanır · (4) bir şirketin yöneticisi diğerinin kayıtlarını görür/onaylar · (5) politika ve saat dilimi paylaşılır |
| **K-2** | Aktivasyon dokunuşu punch mı? | **(a) ADR 0026 kalır** | Spec metni düzeltilir (§2 madde 1) |
| **K-3** | Sandbox | **(a') Ayrı alan adı + ayrı derleme** (1. sürümdeki `sandbox.taptime.mt` + `TAPPA_DEV_TOOLS` tasarımı kurulamaz çıktı — denetim) | Alan adı satın alımı + DNS; ayrı imaj (§4.8) |
| **K-19** | Arka plan işçisi tenant'lar arası tarama yapsın mı? | **Etkinlikle kurulan işçi — tenant'lar arası tarama YOK**, §4.5 değişmez | Pod yeniden başlarsa bir tenant'ın tekrar denemeleri o tenant'ın bir sonraki dokunuşu ya da API çağrısıyla sürer (§4.6) |

**Prod durumu (kullanıcı):** KM tenant'ı prod'da var, dokunuş verisi yok. KM
şirketi KF tenant'ında açılır; KM lokasyon ve çalışanları **KM-ERP'nin kendi API
çağrılarıyla** gelir; KM çalışanları birleşik tenant'ta aktive olur; eski KM
tenant'ı operatör yüzeyinden kapatılır (OP-15 askı yolu ya da karşılığı).

**⚠️ KM plaketleri — API-13 öncesi kullanıcıya sorulacak (§4.5'e değer).**
`tags.uid` **küresel** PK, satır silinmez (silinirse çip kalıcı kilitlenir) ve
`tappa_app`'in `tenant_id` üzerinde UPDATE yetkisi yok. Yeniden encode UID'yi
değiştirmez → aynı UID ile ikinci satır açılamaz; yani 1. sürümdeki "(ii) yeniden
encode" **gerçek bir seçenek değil**. Gerçek seçenekler:
(i) **satırı taşımak** — `tappa_owner` ile koşan, audit'li, **tek seferlik runbook
betiği** (her ortamda koşan goose migration'ı değil); `replaced_by` zinciri
birlikte taşınır; ön koşul: KM tag'lerine bağlı **sıfır** `transactions` satırı
(reject dahil — `transactions_tag_fk (tag_uid, tenant_id)` aksi hâlde taşımayı
engeller ve tek "çözüm" §4.3 tetikleyicisini kapatmak olurdu; **yasak**).
(ii) **yeni fiziksel plaketler** — eski KM satırları arşiv tenant'ta emekli kalır.
Anahtar sarmalının AAD'si yalnız UID olduğu için (i) sarmalı bozmaz; `last_ctr`
geri sarma tetikleyicisi korur. **Kesim sırası:** plaketler taşınmadan (ya da
yenileri takılmadan) hiçbir KM çalışanı birleşik tenant'ta aktive edilmez —
aksi hâlde KM plaketine dokunuş 403 ile **kayıtsız** kalır (§4.6).

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe |
|---|---|---|
| K-4 | Akışa giren punch = `verdict='ok'` ∧ kanal ∈ {`nfc`,`qr`} ∧ ¬`practice`, **veya** onaylanmış `flag`. `reject`/`ignored`/reddedilmiş flag/manuel **asla** | Spec §3.5; §4.6 Tappa içinde karşılanır (kayıt durur, yalnız dışarı gitmez) |
| K-5 | v1'de iptal yolu yok → `version=1`, `status='valid'` | ADR 0011, 0009. "Void" ayrı ürün kararı |
| K-6 | `channel='manual'` ve tarihsel `practice=true` akışa girmez; entegre şirkette manuel kayıt formunda uyarı | Spec §9; çift sayımı önler |
| K-7 | Lokasyon başına saat dilimi sütunu **yok**; farklıysa 422 | Raporlar tenant saat dilimiyle; ikinci kaynak gece vardiyası hatası (§6) |
| K-8 | Dış kimlikler: `emp_`/`loc_`/`pch_`/`dlv_` + UUID; `tag_` + uid | Önek tip karışıklığını yakalar |
| K-9 | Ayrı host `api.taptime.mt` (`TAPPA_API_HOST`), iki yönlü host kapısı (ops yüzeyi kalıbı; mevcut `operatorHostOnly` tek host bildiği için genelleştirilir) | Panel çerezleri (host-only) API host'una gitmez; HTML rotaları orada 404 |
| K-10 | Anahtar biçimi: `tt_live_` öneki + 43 karakter base64url (sandbox'ta `tt_test_`). Depoda kendi env anahtarıyla HMAC-SHA256. **Prod yalnız `tt_live_`**, sandbox yalnız `tt_test_` kabul eder; yanlış önek DB'ye gitmeden 401. `environment` sütunu **yok** (ortamı kurulum belirler) | Mevcut token kalıbı; önek sızıntı taramasını mümkün kılar; bağımsız kimlik = bağımsız env anahtarı |
| K-11 | İmza spec §4.5 biçiminde (`sha256=` öneki + gövdenin hex HMAC-SHA256'sı). **Ek zaman damgası başlığı YOK** | 1. sürümdeki başlık HMAC'e girmediği için sahte güven veriyordu (güvenlik O6); tüketici `id+version` ile tekilleştiriyor |
| K-12 | Webhook adresi ve imza anahtarı **panelden** (owner) yönetilir. Anahtar **yalnız oluşturma ve döndürme anında bir kez** gösterilir — "göster" düğmesi YOK. Döndürmede eski anahtar hemen düşer; bu arada reddedilen teslimler tekrar denemeyle kapanır | Mühürlü değer geri açılabildiği için "göster", ele geçirilmiş panel oturumuna KF-RMS'e sahte imzalı punch yollatırdı (güvenlik O6) |
| K-13 | **İmleç = tenant içinde commit sırasıyla artan `seq`.** Olay satırını tetikleyici yazar; yazmadan önce **tenant başına işlem-ömürlü advisory kilit** alır, `seq`'i küresel bir diziden çeker. 1. sürümdeki `xid8` tasarımı **atıldı** | Ölçüldü: `xid8` imleci `pg_dump` geri yüklemesinden sonra yeni satırları sessizce atlıyordu ve açık tek bir işlem (başka DB'de bile) bütün akışı donduruyordu (üçüncü göz B-3, güvenlik D4). İspat §4.4 |
| K-14 | `employees.location_id` API'den yönetilen satırlarda NULL olabilir: `CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL)`. Panel: bu çalışan için manuel kayıt formu lokasyon seçtirir (`InsertManualTransaction` bugün `e.location_id` okuyor — `db/queries/transactions.sql:354`) | Spec'te ev lokasyonu yok. **API-1 yapıcısı önce `location_id`'nin bütün okuyucularını sayar;** kabul edilemezse geri dönüş: opsiyonel `homeLocationRef` |
| K-15 | E-posta değişince: (a) bekleyen davetler **aynı işlemde** emekliye ayrılır + audit (panel kuralı, ADR 0022 §7); (b) çalışan aktive değilse yeni davet kuyruğa girer. Aktive çalışana davet gitmez | Eski kutudaki kod 7–30 gün geçerli kalıp çalışanı devralmaya izin veriyordu (güvenlik O5) |
| K-16 | API'den yeni çalışan = otomatik davet (panelden eklemenin aksine) | Spec §4.2; panel davranışı değişmez |
| K-17 | Davet **kuyruğa** girer: çalışana `invite_requested_at` yazılır; işçi tavanlar içinde **kodu gönderim anında basar** ve yollar (basma + gönderim kiralanmış tek bir işte — iki pod aynı daveti iki kez basamaz). Kuyrukta **kod yok**. Teslim hatası `invite.undelivered` audit'iyle panelde ve `GET /employees` yanıtında (`activationEmail: queued\|sent\|undelivered`, `activationEmailAt`) görünür. **`resend-activation`** bekleyen davetleri emekliye ayırır (onay vermiş ama dokunmamış çalışan dahil — panel davranışıyla aynı) ve yeni isteği kuyruğa koyar | İlk toplu senkron senkron gönderimde tavanlara çarpardı. ADR 0022 DB outbox'ı **kod saklayacağı için** reddetmişti — burada saklanan yalnız "gönder" isteği |
| K-18 | İlk bağlama: `PUT /employees/{ref}` bilinmeyen ref için önce **aynı şirkette, pasif olmayan, `external_ref`'i boş, e-postası eşleşen** çalışanı bağlar (`employee.linked` audit); yoksa yeni satır açar. Lokasyonlarda otomatik eşleme yok — owner paneldeki "External ref" alanını doldurur | Pilot verisi çift kayda dönüşmesin. Varsayılan şirket KF olduğu ve KM şirketi boş başladığı için KM anahtarı hiçbir KF satırını bağlayamaz |
| K-20 | Lokasyon `active` alanı yalnız etiket: listelerde gizler, yeni plaket bağlamayı engeller; **karar motoruna girmez** | Kararı etkilemesi §5 + politika + ADR ister; v1'de gerek yok |
| K-21 | `employees.updated_at` **tetikleyiciyle** her UPDATE/INSERT'te `clock_timestamp()`; rehber `updated_at >= updatedSince − 10 dk` döndürür ve yanıtta `asOf` verir | Bugün dört ayrı UPDATE yolu var (`invites.sql:250`, `employees.sql:531/612/793`) — elle güncellemek birini unutur. Örtüşme geç commit'i karşılar; rehber küçük ve yazma idempotent |
| K-22 | API ve webhook bir tenant için **yalnız operatör açınca** çalışır: `tenants.api_enabled_at`, tek yazarı yeni bir `op_*` fonksiyonu (ADR 0021 sınıfı, `operator_audit_log`'a iz). Kapalı tenant'ın anahtarı 403 | `/signup` herkese açık: her yabancı bir tenant açıp sunucumuza istediği URL'ye istek attırabilirdi (güvenlik O4). Bugün tek entegratör KF |
| K-23 | API kaynaklı audit satırları: `actor_id = api_keys.id`, `detail.via = "api"` | `audit_log.actor_id` FK'siz; aktör bir admin değilken izin kaynağı yine adlandırılmalı |
| K-24 | **Şirket sınırı her sorguda:** API yazma/okuma araması `(tenant_id, company_id, external_ref)` üçlüsüyle; tekillikler: çalışan `UNIQUE (tenant_id, company_id, external_ref) WHERE status <> 'deactivated'`, lokasyon `UNIQUE (tenant_id, company_id, external_ref)`; e-posta `UNIQUE (tenant_id, company_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'` | Şirket sınırı RLS'te değil uygulamada (aynı tenant) — üçlü arama olmadan KM anahtarı bir KF çalışanını geri alınamaz biçimde pasifleştirebilirdi; tenant genelindeki e-posta tekilliği de KM anahtarına KF adresleri için 409 kehaneti veriyordu (iki denetim). Pasif satırı dışlamak yeniden işe alımı ve KF↔KM geçişini mümkün kılar |

---

## 4. Tasarım özü (ADR 0027 ve 0028'de normatif olacak)

### 4.1 Şirket boyutu (K-1 = A)

- `companies(tenant_id, id, code, name, vat_number NULL, created_at)` — RLS beşlisi;
  `UNIQUE(tenant_id, code)`. Her mevcut tenant'a migration'da **bir varsayılan
  şirket**. Kod owner tarafından düzenlenebilir (KF tenant'ının varsayılanı canlı
  öncesi `KF` yapılır). Tek şirketli müşteri farkı görmez.
- `locations.company_id`, `employees.company_id` — NOT NULL, varsayılanla doldurulur,
  bileşik FK `(company_id, tenant_id)`.
- Tap motoru **değişmez**: şirket karar girdisi değildir; dokunuş tenant içidir.
- Panel: şirket yönetimi (owner), listelerde şirket sütunu/filtresi, çalışan
  eklerken şirket seçimi. Panel yetkileri tenant geneli kalır (K-1 bedeli 4);
  EM-12 müdür rolünü tasarlarken şirket kapsamını ele alır.

### 4.2 API anahtarı

- `api_keys(tenant_id, id, company_id, name, token_hash, prefix_hint, created_by, created_at, last_used_at, revoked_at)` —
  RLS beşlisi; `token_hash` **küresel UNIQUE**.
- Kimlik: Bearer → biçim kontrolü (önek + 43 base64url; ortamın öneki değilse DB'ye
  gitmeden 401) → **yedinci resolver** `resolve_api_key_by_hash` (`tappa_resolver`
  sahipli SECURITY DEFINER; ADR 0002 §7'nin beş şartı: girdi yalnız hash, dönüş
  UNIQUE indeksle tek satır, sabit `search_path`, nitelikli adlar, dinamik SQL yok).
  **İptal ve tenant'ın API erişimi fonksiyonun içinde süzülür** (`revoked_at IS NULL`,
  `api_enabled_at IS NOT NULL`); dönüş `(tenant_id, key_id, company_id)`. **Önbellek
  yok** — iptal bir sonraki istekte etkili.
- Sonraki her sorgu `WithTenant` + açık `tenant_id` + **`company_id`** filtresi (K-24).
- Yetki (spec §4 tablosu): yazma = anahtarın şirketinin çalışan/lokasyonu ·
  okuma = `scope=location` (şirketin lokasyonlarındaki tüm punch'lar) +
  `scope=employer` (şirketin çalışanlarının her yerdeki punch'ları) + rehber (tenant geneli).
- Panel (owner): oluştur (bir kez göster) · listele (ad, önek ipucu, son kullanım) ·
  iptal. Audit: `api_key.created`, `api_key.revoked`. `last_used_at` dakikada en çok bir kez.
- **Oran sınırı iki katlı:** IP başına kova **resolver'dan önce** (geçersiz
  anahtar selinin her isteği bir HMAC + bir DB çağrısı yapıp tap havuzunu
  yormasın — güvenlik O3); geçerli anahtarda anahtar başına kova.
- **Log yasağı:** anahtar, hash'i, `Authorization` başlığı. R7 tetik listesine
  `api_?key|bearer|authorization|signature` kökleri, R7d'ye anahtar önekli değer
  kalıbı eklenir (API-3'ün işi). CLAUDE.md §7 yasak listesine API anahtarı ve
  webhook imza anahtarı eklenir (API-0).

### 4.3 Punch olayı — yapısal yayın

Dış dünyadaki punch, `transactions` satırının **dışarıya bakan izdüşümüdür**.
İzdüşüm doğduğunda (ileride değiştiğinde) **append-only** bir olay yazılır.

- `punch_events(tenant_id, seq bigint, punch_id, version, status, created_at)` —
  bileşik FK `(punch_id, tenant_id) → transactions`; `transactions` gibi
  UPDATE/DELETE/TRUNCATE tetikleyicileri; R3 tarayıcısı bu tabloyu da kapsar.
- **Olayı veritabanı tetikleyicisi yazar** (1. sürümde Go katmanıydı — denetim
  B-2: migration ile pod değişimi arasında eski binary'nin ve her rollback'in
  yazdığı punch'lar **hiç olay almıyordu**):
  - `AFTER INSERT ON transactions` — K-4 koşulu sağlanırsa `version=1`;
  - `AFTER INSERT ON transaction_reviews` — `outcome='approved'` ise `version=1`.
  Binary sürümünden bağımsızdır; saf `tap.Decide`, `policy` ve sayaç ilerletme yolu
  **dokunulmaz**.
- **Aynı tetikleyici teslim satırlarını da yazar** (§4.5): olay, tenant bağlamı
  içinde, eşleşen aktif uç noktalar için `webhook_deliveries` satırı doğurur.
  Ayrı bir "yayılım" taraması **yoktur**.
- **Geri doldurma:** tetikleyiciyi kuran migration, aynı işlemde (tablo kilidi
  altında) geçmiş uygun satırlar için olay yazar — sıra `occurred_at`. Tetikleyici
  kurulduktan sonraki her INSERT tetikleyiciden geçer; arada boşluk kalmaz.
  Geri doldurulan olaylar için teslim satırı **yazılmaz** (geçmiş webhook'a itilmez).
- **Bedel (adlandırılmış):** tetikleyicinin her hatası `ok` dokunuşun kaydını geri
  alır — entegrasyonu olmayan tenant'larda da. Tetikleyici bu yüzden en küçük hâlinde
  tutulur ve API-2 bu davranışın testini taşır.

### 4.4 Akış imleci — neden kaybolmaz

Tetikleyici olayı yazmadan önce `pg_advisory_xact_lock(<tenant'a özgü anahtar>)`
alır, sonra `seq`'i küresel diziden çeker. Kilit commit'e kadar tutulur. Sonuç:
aynı tenant'ta iki olay-yazan işlem **kilidi sırayla** alır, dolayısıyla `seq`'i de
sırayla alır ve **o sırayla commit eder**. Bir okuyucu `seq = S`'ye kadar commit
edilmiş satırları gördüğü anda, o tenant'ta henüz commit etmemiş olay en çok bir
tanedir (kilidi tutan) ve onun `seq`'i `S`'den büyüktür; sonrakiler daha da büyük.
Yani `WHERE tenant_id = $t AND seq > $cursor ORDER BY seq` **hiçbir satırı atlamaz**.
Abort eden işlem bir `seq` boşluğu bırakır — boşluk zararsız.

- **Sorgu kapsamı tenant içidir** (kapsam filtreleri şirket bazlı, hep tek tenant)
  → küresel dizi, tenant başına sıralı alt dizi verir; bu yeter.
- **Geri yükleme:** dizi değeri `pg_dump`'la birlikte gelir. Ama yedek anından
  sonra tüketilmiş olaylar varsa tüketicinin imleci geri yüklenen değerin
  **ötesindedir** ve yeni olaylar o aralığı yeniden kullanır. Bu yüzden **geri
  yükleme betiği diziyi büyük bir aralık ileri atar** (yapısal adım, elle değil;
  `scripts/pg-restore-verify.sh` ve geri yükleme runbook'u). Kabul testi: geri
  yüklenmiş bir kopyada yeni olay, eski imlecin ötesinde döner.
- **Kilit sırası değişmezi:** tenant akış kilidi bir işlemde alınan **son** kilittir
  (checkin'in kişi başına kilidinden sonra). Bunu tersine çeviren yol olmamalı
  (kilitlenme); API-2'de test edilir. Kilit **iki argümanlı** biçimde, akışa özel
  bir sınıf numarasıyla alınır — kişi başına kilidin anahtar uzayıyla çakışıp
  beklenmedik bekleme ya da kilitlenme üretmesin. Tenant'lar arası hash çakışması
  yalnız fazladan sıralama demektir, zararsız.
- **Bedel:** aynı tenant'ın olay-yazan işlemleri commit'te sıraya girer. Kilit,
  kayıt INSERT'inden commit'e kadar tutulur — checkin'de birkaç ifade. Dokunuş
  hacmi (dakikada onlarca) için ihmal edilebilir; API-2 süreyi ölçer.
- İmleç opak (base64) ve yalnız `seq` taşır. Tenant ve kapsam filtresi her sorguda ayrıca.
- Geçmiş sorgusu (`from`/`to`): `occurred_at` aralığı, aynı yayın kuralı,
  `punch_events` üzerinden; aralık ≤ 31 gün, sayfalı.

### 4.5 Webhook

- `webhook_endpoints(tenant_id, id, company_id, url, signing_key_sealed, active, created_at, rotated_at)`
  — anahtar `sun.Seal(TAPPA_WEBHOOK_KEK, AAD = endpoint id)` ile mühürlü (operatör
  TOTP emsali; **kendi KEK'i**, `TAPPA_TAG_KEK`'ten ayrı olduğu config'te zorlanır).
- `webhook_deliveries(tenant_id, id, endpoint_id, punch_event_seq, attempt, next_attempt_at, lease_until, state, last_status, last_error_class, created_at, delivered_at)`
  — `UNIQUE(endpoint_id, punch_event_seq)`; `state ∈ pending|delivered|dead`.
  `last_error_class` **kapalı bir küme** (timeout · tls · refused · http_4xx ·
  http_5xx · blocked_address); uzak sunucunun yanıt metni **saklanmaz, loglanmaz**.
- **Yönlendirme (spec §4.5):** olay → lokasyonun şirketinin uç noktası ∪ çalışanın
  işvereninin uç noktası (aynıysa tek). Teslim satırını §4.3'teki tetikleyici yazar.
  Yeni bir uç nokta **yalnız oluşturulduktan sonraki** olayları alır.
- **Yük izin listesi:** spec §5 alanları + `channel`. IP, GPS, mesafe, not, policy
  bağlamı **asla**. Test ping sentetiktir (`event: "ping"`, gerçek punch değil) ve
  tenant başına saatte 10 ile sınırlı.
- **İşçi — kiralama modeli:** (1) vadesi gelen teslimi kısa bir işlemde
  `FOR UPDATE SKIP LOCKED` ile seç, `lease_until` yaz, **commit**; (2) HTTP isteğini
  işlem **dışında** gönder; (3) sonucu ayrı kısa bir işlemde yaz. HTTP boyunca
  açık işlem ya da tutulan havuz bağlantısı yok (güvenlik D4). Rollout'ta iki pod
  aynı teslimi kira yüzünden bir kez gönderir. Geri çekilme 1 dk · 5 dk · 30 dk ·
  2 sa · 6 sa → `dead`. Panelde teslim günlüğü + "yeniden gönder".
- **İşçi kurulumu — K-19, tenant'lar arası tarama YOK:** süreç içi bir "işi olan
  tenant'lar" kümesi. Bir tenant kümeye şu anlarda girer: o tenant'ta bir
  dokunuş/onay commit edildiğinde, o tenant'ın kimliği doğrulanmış her API
  isteğinde, panelden "yeniden gönder"de. Girince işçi **`WithTenant` içinde** o
  tenant'ın vadesi gelmiş teslimlerini ve davet isteklerini okur, zamanlayıcı kurar;
  işi bitince tenant kümeden düşer. **Pod yeniden başlarsa** küme boştur; bir
  tenant'ın bekleyen tekrar denemeleri o tenant'ın bir sonraki dokunuşu ya da API
  çağrısıyla sürer (entegratörler akışı düzenli çeker — spec §9). Bu bedel
  kullanıcı kararıdır.
- **SSRF — izin listesi, yasak listesi değil:**
  - yalnız `https`, port 443, URL'de IP literali yok;
  - **dial anında** çözülen her adres `Unmap()` edilir ve şu şartın **hepsini** sağlamalı:
    küresel unicast; IANA özel amaçlı kayıtlarında değil (en az: `0.0.0.0/8`,
    `10/8`, `100.64/10`, `127/8`, `169.254/16`, `172.16/12`, `192.0.0.0/24`,
    `192.168/16`, `198.18/15`, `240/4`, `::`/`::1`, `fc00::/7`, `fe80::/10`,
    `64:ff9b::/96`, `2002::/16`, multicast); **düğümün kendi genel adresleri ve
    ingress adresi değil** (config listesi — ingress `hostNetwork` olduğu için düğüm
    adresine giden istek iç ağ kaynağıyla nginx'e ulaşır);
  - `Transport.Proxy: nil` açıkça (ortam değişkeni proxy'si `Control`'ü atlatmasın);
    yönlendirme izlenmez; toplam 10 sn; `MaxResponseHeaderBytes` sınırlı; gövde
    **okunmaz** (en çok 1 KiB atılır); TLS doğrulaması varsayılan;
  - **tehdit modeli ADR 0028'de yazılı:** URL'yi owner girer; tenant'ı herkes
    açabilir → K-22 kapısı + test ping sınırı.
  - Bugün egress NetworkPolicy yok — bu kapı tek savunma; egress politikası backlog adayı.
- **Kapanış bütçesi:** işçi `srv.Shutdown` ile **eşzamanlı** boşalır, 20 sn
  penceresinin içinde (sıfırlama kuyruğu emsali); sıralı ek süre YOK — bugün pay
  sıfır (`httpShutdownGrace 20 s + encode.DefaultCloseGrace 5 s`, testin istediği
  ≥ 5 sn pay ile `terminationGracePeriodSeconds: 30`). Uçuştaki istek iptal edilir,
  kira dolunca teslim yeniden denenir. Bunu sabitleyen test API-10'da.

### 4.6 Ortak sözleşme

- JSON hata gövdesi `{code, message, details}`; 400/401/403/404/409/422/429/503;
  429'da `Retry-After` (mevcut `httpx` davranışı). `limit` ≤ 1000 (varsayılan 500).
  İstek gövdesi ≤ 64 KiB.
- **Idempotency-Key** (yazma isteklerinde opsiyonel):
  `api_idempotency(tenant_id, key_id, idem_key, request_hash, status, result_ref, created_at)`.
  **İlk yazan kazanır:** işlemden önce `INSERT … ON CONFLICT DO NOTHING`; çakışan
  eşzamanlı istek 409 `idempotency_in_progress`. Yanıt gövdesi **saklanmaz**, yalnız
  durum + oluşan kaydın kimliği (kişisel veri kopyası yok). Aynı anahtar + farklı
  gövde → 422. 24 sa sonra **istek yolunda, o tenant için tembel** silinir
  (tenant'lar arası temizlik işi yok — K-19).
- OpenAPI 3.1 **elle yazılır**, embed edilir, `GET /v1/openapi.yaml` (Node yok, üretici yok).
- İmza için Tappa'nın kendi bilinen-cevap vektörü üretilir ve rehbere konur (spec'te vektör yok).

### 4.7 Askıya alma (ADR 0025 ile hizalı)

ADR 0025 askıyı **yalnız bir yazma kilidi** olarak tanımlar; okumalar, CSV ve
kaydın telafi yolları (kadroya yeni kişi, yeniden davet, manuel kayıt) askıyı
bilmez (kullanıcı kararı K-B1). API aynı ayrımı uygular:

| Açık kalır | Askıda 403 `tenant_suspended` |
|---|---|
| bütün `GET`'ler, `/v1/punches`, `/v1/directory`, webhook gönderimi, `PUT /employees` (**yeni** çalışan), `resend-activation` | `PUT /locations`, var olan çalışanı değiştiren `PUT /employees`, panelden anahtar/uç nokta yönetimi |

`DELETE /employees` (pasifleştirme) askıda **açık** kalır: ayrılan çalışanın
dokunmaya devam etmesi bir güvenlik riskidir ve deaktivasyon kayıt yaratmaz,
engeller. Bu ADR 0025'in listesine **eklenen** bir satırdır → API-0'da ADR 0025'e
not olarak yazılır. OP-15 hâlâ beklemede; migration numarası (00035) çakışıyor,
M11 onu beklemez, OP-15 yeniden numaralanır.

### 4.8 Sandbox (K-3 = a')

- **Ayrı alan adı**, `taptime.mt`'nin alt alanı **değil** (`internal/handler/cookies.go:138-159`
  kalıcı kısıtı: güvenilmeyen alt alan çerez sorusunu yeniden açar).
- **Ayrı derleme:** simülasyon kodu bir build tag'iyle yalnız sandbox ikilisine
  girer; prod imajı onu **yapısal olarak** içermez (test: prod derlemesinde
  simülasyon sembolü/rotası yok). Sandbox ikilisi yalnız `TAPPA_ENV=sandbox` ile
  başlar; prod ikilisi bu değeri reddeder. Mevcut dev simülatörünün dört katlı
  kapısı **gevşetilmez**.
- **Simülasyon yüzeyi:** yalnız sandbox'ta `POST /v1/sandbox/taps {employeeRef, locationRef, channel}`
  → gerçek checkin yolunu sandbox'ın kendi sanal plaketiyle koşturur.
- **Sırlar ve veri:** sandbox kendi sırlarıyla kurulur; **prod yedeği sandbox'a
  geri yüklenmez** (runbook) ve sandbox ikilisi `encoded_at` dolu (gerçek, encode
  edilmiş) bir plaket satırı görürse **başlamayı reddeder** — ortak KEK + prod
  satırı ile sandbox'ta sayacı ilerletip prod'da geçerli bir SUN URL'si basma
  zinciri böylece yapısal olarak kapanır.
- **E-posta:** sandbox gerçek e-posta göndermez; davet `panel` modunda kalır, aktivasyon
  linki sandbox'a özel bir uçtan alınır.
- **Kaynak:** aynı k3s düğümünde ayrı namespace + Postgres, `ResourceQuota`/`LimitRange` ile sınırlı.

---

## 5. Görevler

| ID | Görev | Boyut | Ajan | Bağımlılık |
|---|---|---|---|---|
| API-0 | ADR 0027 (şirket boyutu) + ADR 0028 (dış API: anahtar, punch yayını, imleç, webhook, SSRF tehdit modeli, K-19 işçisi, sandbox kapısı) + notlar: ADR 0026 (K-2), ADR 0022 §7 (otomatik davet + kuyruk), ADR 0010 (API `DELETE` iki adımlı panel onayını atlar), ADR 0025 (§4.7 tablosu), ADR 0002 §7 (resolver sayısı 7), ADR 0021 (`op_*` yeni fonksiyon K-22) · CLAUDE.md §3 (yeni paketler) ve §7 (log yasakları) | M | yapıcı + üçüncü göz | — |
| API-1 | Şema: `companies`, `company_id`'ler, `external_ref`, `employee_code`, `first_name`/`last_name`, `employees.updated_at` + tetikleyici (K-21), `invite_requested_at`, `locations.active`, K-14 CHECK, K-24 tekillikleri (e-posta indeksinin değişimi dahil), `tenants.api_enabled_at` + `op_*` yazarı (K-22). Panel: şirket yönetimi, şirket seçimi, "External ref" alanları, manuel kayıtta lokasyon seçimi | L | `tappa-db-migrator` + yapıcı (`tappa-brand`) | API-0 |
| API-2 | `punch_events` + iki tetikleyici (olay + teslim satırı) + tenant kilidi + geri doldurma + geri yükleme betiğinde dizi atlatma | M | `tappa-db-migrator` | API-1 |
| API-3 | `api_keys` + `resolve_api_key_by_hash` + `internal/apikey` (redakte tip) + panel sayfası (owner) + audit + R7/R7d kalıpları + `namedKeys`/ayrım kümesine yeni env anahtarları | M | yapıcı (`tappa-brand`) | API-1 |
| API-4 | `/v1` iskeleti: host kapısı (iki host'u bilen genelleştirme), biçim kontrolü + IP kovası + Bearer + anahtar kovası, JSON hata, gövde sınırı, Idempotency-Key, askı tablosu (§4.7), `openapi.yaml` iskeleti, K-19 işçi iskeleti (küme + kiralama + kapanış) | M | yapıcı | API-3 |
| API-5 | `PUT /v1/locations/{ref}` (K-7, K-20, K-24) | S | yapıcı | API-4 |
| API-6 | `PUT/GET/DELETE /v1/employees/{ref}`, `resend-activation`; K-14…K-18, K-24; davet kuyruğu işçisi | L | yapıcı | API-4, **EM-5B canlıda** |
| API-7 | `GET /v1/punches` (iki kapsam, imleç, `from/to`) | M | yapıcı | API-2, API-4 |
| API-8 | `GET /v1/directory?updatedSince` (K-21) | S | yapıcı | API-1, API-4 |
| API-9 | `webhook_endpoints` + mühürlü imza anahtarı + SSRF-güvenli istemci + panel (adres, oluştur/döndür — bir kez göster, test ping) | M | yapıcı (`tappa-brand`) | API-1, API-4 |
| API-10 | Webhook gönderici: kiralama, geri çekilme, `dead`, yeniden gönder, kapanış testi, panel teslim günlüğü | L | yapıcı (`tappa-brand`) | API-2, API-9 |
| API-11 | Sandbox ikilisi + simülasyon ucu + kurulum (§4.8) | M | yapıcı + kullanıcı (alan adı/DNS) | API-5, API-6, API-7, API-8, API-10 |
| API-12 | OpenAPI tamamı + entegrasyon rehberi (EN; §2 farkları + imza vektörü) | S | yapıcı | API-5…API-10 |
| API-13 | Canlıya alma: sırlar, `api.` DNS + Ingress, **`tappa-security-auditor` tam tur**, operatörden K-22 açılışı, KF bağlama runbook'u (K-18), KM şirketi + KM plaketleri (§3 — kullanıcıya sorulur), eski KM tenant'ının kapatılması, spec §7 kabul listesi | M | yapıcı + denetçi + kullanıcı | hepsi + **EM-12** |

Her görev: yapıcı (opus) → **ayrı** üçüncü göz → bulgu varsa düzelt + yeniden
denetle (CLAUDE.md §10). Büyük testler (`-race` tamamı, `make check`) yalnız görev
sonunda bir kez. Dal: `m11-api` (`main`'e birleştirme = deploy kararı, kullanıcının).
Her commit'ten önce `./scripts/redline-check.sh` — pre-push kancası push edilen
**her** commit'i tarar, sonradan silmek yetmez.

### Kabul çekirdeği (kart açılırken genişler — burada bağlayıcı)

- **API-0:** her ADR notu var; CLAUDE.md §4.5 metni **değişmemiş** (K-19); ADR 0028'de SSRF tehdit modeli ve sandbox kapısı normatif.
- **API-1:** `\d` ile her yeni tablo RLS beşlisi + GRANT; mevcut her tenant'ın tam bir varsayılan şirketi var; K-24 indeksleri; pasif satırla aynı e-postada yeni aktif satır **açılabiliyor**, iki aktif satır **açılamıyor**; `location_id` okuyucu sayımı rapora yazılmış; `updated_at` dört UPDATE yolunun dördünde de değişiyor (her biri için test); `api_enabled_at`'ı `tappa_app` yazamıyor.
- **API-2:** (a) **eski binary simülasyonu:** olay kodunu bilmeyen düz bir `INSERT INTO transactions` yine olay doğurur; (b) **geç commit:** aynı tenant'ta iki işlem, ilki kilidi alıp bekler → okuyucu ikinciyi ilkinden önce **göremez**, ikisi de commit olunca sırayla alır (kilitsiz tetikleyiciyle aynı test **kırmızı** — mutasyon kanıtı); (c) **geri yükleme:** geri yüklenmiş kopyada yeni olay eski imlecin ötesinde döner; (d) flag yazan dokunuş olay yazmaz, onaylanınca yazar, reddedilince yazmaz; manuel satır asla; (e) B tenant'ı A'nın olaylarını görmez (filtresiz sorgu); (f) tetikleyici hatası dokunuş kaydını geri alır (adlandırılmış bedel); (g) kilit sırası testi.
- **API-3:** iptal edilmiş anahtar bir sonraki istekte 401; `api_enabled_at` boş tenant'ın anahtarı 403; yanlış ortam öneki DB'ye gitmeden 401; anahtar log'da/hata metninde/audit'te yok, yeni R7 kökleri yeşil ve **kasıtlı ihlalde kırmızı**; B'nin anahtarı A'nın tek satırını okuyamaz/yazamaz.
- **API-4:** API host'unda `/admin` 404, ana host'ta `/v1` 404; geçersiz anahtar selinde IP kovası resolver'dan önce 429 verir; aynı Idempotency-Key + farklı gövde 422; eşzamanlı aynı anahtar tek işlem; §4.7 tablosunun her satırı için askı testi.
- **API-5:** KM anahtarı KF lokasyonunu **okuyamaz/değiştiremez** (aynı `externalRef` ile bile — yeni KM satırı açar); farklı `timezone` 422; tekrar `PUT` yeni satır açmaz.
- **API-6:** KM anahtarı KF çalışanını okuyamaz/değiştiremez/**pasifleştiremez**; KF çalışanının e-postasıyla KM'de çalışan açmak 409 **vermez** (K-24); pasif çalışana `active:true` → yeni satır, eski geçmiş yerinde; e-posta değişince eski davet aynı işlemde ölü; aktive çalışana `resend-activation` 409; kuyrukta **kod yok** (şema + log taraması); iki pod aynı daveti bir kez basar.
- **API-7:** `scope=location` KM çalışanının KF lokasyonundaki punch'ını KF anahtarına döndürür, `scope=employer` KM anahtarına; manuel ve reddedilmiş satır hiçbir kapsamda yok; `limit` 1001 → 400.
- **API-8:** `updatedSince` örtüşme penceresi içindeki değişikliği kaçırmaz; yanıtta e-posta yok.
- **API-9:** uç nokta anahtarı ikinci kez gösterilemez; `op_*` fonksiyonları `signing_key_sealed` ve `token_hash` sütunlarını **göremez**; ping tenant başına sınırlı.
- **API-10:** `169.254.169.254`, `0.0.0.0`, `::ffff:127.0.0.1`, `64:ff9b::` + iç adres, düğüm adresi, `localhost`'a çözülen ad, `http://`, 302 → reddedilir; `HTTP_PROXY` tanımlıyken bile doğrudan bağlanır; aynı teslim iki pod'dan bir kez gider; gönderim sırasında açık DB işlemi yok; imza kendi vektörümüzle doğrulanır; kapanış bütçesi testi yeşil.
- **API-11:** prod derlemesinde simülasyon yok; sandbox ikilisi `encoded_at` dolu plaketle başlamaz; prod ikilisi `TAPPA_ENV=sandbox`'ı reddeder.
- **API-12:** OpenAPI her uç ve hata kodunu kapsar; §2'nin 18 maddesi rehberde.
- **API-13:** spec §7 listesinin dokuz maddesi tek tek işaretli; güvenlik denetimi ONAY.

---

## 6. Riskler ve tuzaklar (ölçülmüş)

- **R7 log tarayıcısı** `token|secret|…` geçen her log çağrısını düşürür; R7d plan/doküman satırlarını da tarar (bu dosyanın 1. sürümü yakalandı ve push edilemedi).
- **Tek replika, bellekte limitler ve K-19 kümesi:** süreç başına; rollout'ta 2× patlama mümkün (T1); işçi kümesi yeniden başlamada boşalır (kabul edilmiş bedel).
- **EM-5B** `m10-a1`'de commit'lendi; canlıya çıkmadan API-6 kuyruğu yalnız `undelivered` üretir.
- **Davet tavanları ve 300/saat süreç kesicisi** ilk toplu senkronu saatlere yayar — runbook'ta yazılır, KF-RMS ekibine önceden söylenir.
- **Pilot verisinin çiftlenmesi** → K-18; API-13 runbook'u ilk `PUT`'tan **önce** panel eşlemesini ister.
- **K-1 bedelleri** (§3) ve **EM-12 ön koşulu**.
- **Uzun işlem:** `idle_in_transaction_session_timeout` bugün 0; akış artık xmin'e bağlı değil ama takılı bir işlem havuz bağlantısı tutar — uygulama rolü için zaman aşımı + alarm sertleştirme adayı.
- **GDPR:** rehber iki şirketin çalışan adlarını karşılıklı açar (spec istiyor); DPA/aydınlatma metni güncellemesi kullanıcıda (Q13 ile birlikte).
- **Egress açık:** SSRF kapısı tek savunma.

## 7. Kullanıcının dış adımları

1. ~~K-1, K-2, K-3, K-19~~ ✅ 2026-10-09.
2. Sandbox için `taptime.mt` dışında bir alan adı + DNS; `api.taptime.mt` DNS kaydı (Cloudflare, DNS-only).
3. KF-RMS / KM-ERP ekibinden canlı + test webhook adresleri (spec §8).
4. Canlı öncesi: paneldeki KF lokasyonlarına `externalRef` girilmesi (K-18); KF tenant'ının varsayılan şirket kodunun `KF` yapılması.
5. KM plaketlerinin akıbeti (taşıma mı, yeni plaket mi) — API-13 öncesi sorulacak.
6. Eski KM tenant'ının kapatılması (operatör).

## 8. Kapsam dışı (v1)

Punch yazma API'si (spec §9) · punch iptali/düzeltmesi · departman uçları ·
lokasyon silme · webhook yönetimi API'den · lokasyon başına saat dilimi · şirket
başına marka · şirket kapsamlı panel yetkisi (EM-12'nin işi) · OAuth · çoklu
replika için paylaşımlı oran deposu · egress NetworkPolicy.

---

## 9. Denetim izi

**1. tur (2026-10-09, `a169391`) — iki bağımsız denetim, ikisi de RED.**

| Bulgu | Kaynak | Nereye işlendi |
|---|---|---|
| Tenant'lar arası tarama yeni §4.5 istisnası, kullanıcıya sorulmadı | üçüncü göz B-1, güvenlik B1 | K-19 (kullanıcı kararı: tarama yok) · §4.5 işçi kurulumu · §4.6 tembel temizlik |
| Olay Go katmanında → deploy/rollback'te kayıp | üçüncü göz B-2 | §4.3 tetikleyici · API-2 (a) |
| `xid8` imleci geri yüklemede atlıyor (ölçüldü) | üçüncü göz B-3 | K-13 tenant kilidi + dizi · §4.4 geri yükleme adımı · API-2 (c) |
| Askı ADR 0025 ile çelişiyor | üçüncü göz B-4, güvenlik O7 | §4.7 tablosu |
| Sandbox kapısı kurulamaz / prod'a risk / alt alan çerez kısıtı | güvenlik B2, üçüncü göz O-2 | K-3 (a') · §4.8 · API-11 |
| Şirket sınırı yalnız uygulamada; e-posta kehaneti; pasif satır tekilliği | güvenlik O1, üçüncü göz O-5, D-10 | K-24 · API-5/6 kabul |
| KM plaketi "(ii) yeniden encode" gerçek değil; FK/`replaced_by`/kesim sırası | güvenlik O2, üçüncü göz O-3 | §3 KM plaketleri |
| Kimliksiz istek sınırı yok | güvenlik O3 | §4.2 iki katlı sınır · API-4 kabul |
| SSRF yasak listesi eksik; tehdit modeli yok | güvenlik O4 | §4.5 izin listesi · K-22 |
| E-posta değişince eski davet yaşıyor; aktive çalışana resend | güvenlik O5, üçüncü göz D-6 | K-15 · K-17 · §2 madde 13 |
| İmza anahtarı "göster"; zaman damgası imzasız | güvenlik O6 | K-11 · K-12 · API-9 kabul |
| Kapanış bütçesi formülü yanlış | üçüncü göz O-1 | §4.5 kapanış |
| K-1 bedelleri eksik | üçüncü göz O-4, güvenlik D8 | §3 K-1 (kullanıcı yeniden onayladı) · EM-12 ön koşulu |
| `updated_at` belirsiz, geç commit | üçüncü göz O-6 | K-21 |
| `active:false` anlamsız | üçüncü göz O-7 | K-20 · §2 madde 8 |
| Eksik ADR notları | üçüncü göz O-8 | API-0 |
| R7 kökleri, resolver iptali, `token_hash` küresel tekillik | güvenlik D1, D2 | §4.2 · API-3 |
| Tetikleyici hatası kaydı geri alır | güvenlik D3 | §4.3 bedel · API-2 (f) |
| HTTP boyunca açık işlem | güvenlik D4, üçüncü göz D-5 | §4.5 kiralama |
| `punch_events` FK / R3 | güvenlik D5 | §4.3 |
| Idempotency gövdesi kişisel veri; eşzamanlılık | güvenlik D6 | §4.6 |
| Yeni uç noktanın başlangıcı; yük izin listesi | güvenlik D7 | §4.5 |
| Yeni anahtarlar ayrım kümesi dışında | güvenlik D9 | §4.5 · API-3 |
| `punchTime` beyan; QR `tagId`; 429↔kuyruk çelişkisi; spec vektörü; debounce; 31 gün; aktör; eksik görevler/kabul; `environment` sütunu; çalışan 3/saat tavanı; `Retry-After` zaten var | üçüncü göz D-1…D-11, A-6, §1 | §1 · §2 madde 4/5/6/11/16 · K-10 · K-23 · §4.6 · §5 |
