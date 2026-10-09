# M11 — Entegrasyon API'si (KF-RMS · KM-ERP)

> **Durum:** PLAN (2026-10-09, 28. oturum). Kod yok. Kullanıcı kararları
> **verildi** (K-1 = A, K-2 = a, K-3 = a — §3); sıradaki adım API-0 (ADR'ler).
>
> **Kaynak:** kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve
> KM-ERP entegrasyonu)"*, 2026-10-09. **Repoya konmadı** — depo public ve doküman
> müşterinin iç webhook adresini taşıyor. Bu dosyada "spec §N" o dokümanın
> bölümüdür.

**Amaç.** Tenant'ın kendi panelinden ürettiği **API anahtarlarıyla** dış İK/ERP
sistemlerinin (ilk müşteri: KF-RMS ve KM-ERP) Tappa'ya lokasyon ve çalışan
göndermesi, punch'ları **imleçli akıştan** çekmesi ve **webhook** ile anında
alması. ZKBio'nun yerini alan entegrasyon yüzeyi budur.

**Neden şimdi / neden ayrı milestone.** Bugün dış dünyaya açık hiçbir JSON API,
Bearer kimliği, webhook, OpenAPI ya da Idempotency-Key yok (ölçüldü, 2026-10-09).
Bu iş yeni bir **güvenlik sınırı** (makineden makineye kimlik), yeni bir **giden
ağ yüzeyi** (müşterinin seçtiği URL'ye istek) ve yeni bir **sıralama sözleşmesi**
(punch akışı) açar — üçü de ADR ister (CLAUDE.md §10).

---

## 1. Spec ↔ Tappa — bugünkü durum (ölçüldü)

| Spec beklentisi | Tappa bugün | Plan |
|---|---|---|
| İki şirket (KF/KM), **her çalışan iki şirketin her lokasyonunda okutabilir** | KF ve KM **iki ayrı tenant**; tenant'ları gruplayan kavram yok. Yabancı tenant plaketi → `sys:tenant-mismatch` → **403, kayıt yok** (`internal/policy/guardrails.go:306-319`) | **K-1** |
| Location: `externalRef`, `timezone`, `active` | external id yok · saat dilimi **tenant** sütunu · aktif bayrağı yok, silme sert `DELETE` | API-1, K-7 |
| Tag panelden lokasyona bağlanır | Var (`tags.uid` PK, `location_id`, durum) | Değişmez; `tagId = tag_<uid>` |
| Employee: `externalRef`, `employeeCode`, `firstName`, `lastName`, `email`, `active`, `activated` | external id/kod **yok** · tek `full_name` · `email citext` **var** (tenant başına tekil) · `status` + `activated_at` **var** · `location_id NOT NULL` (spec'te karşılığı yok) | API-1, K-14 |
| Yeni çalışana **otomatik** aktivasyon e-postası | Çalışan eklemek davet **göndermez**; davet ayrı düğme. E-posta **prod'da kapalı** (`TAPPA_INVITE_DELIVERY=panel`, EM-5B bekliyor). Davet tavanları: tenant 50/saat 300/gün, kutu 5/saat 20/gün | API-6, K-16, K-17 |
| **İlk dokunuş = ilk punch** | ADR 0026: aktivasyon dokunuşu **`transactions` satırı yazmaz** | **K-2** |
| "Doğrulanamayan dokunuş punch'a dönüşmez" | `flag` kaydı **yazılır** ve onay kuyruğuna düşer (§4.6) | K-4 |
| Düzeltme/iptal → yeni `version` | Geçerli bir kaydı iptal yolu **yok**; düzeltme = bağsız yeni manuel satır (ADR 0011) | K-5 |
| Manuel punch'lar RMS/ERP'de (spec §9) | Tappa'da `channel='manual'` kayıt var | K-6 |
| İmleçli akış, **kayıt sırasına göre**, geç gelen kaçmaz | `transactions`'ta **hiçbir monoton anahtar yok** (hepsi `gen_random_uuid()`), `created_at` = işlem başlangıcı ≠ commit sırası | API-2 (K-13) |
| Webhook + imza + tekrar deneme | DB outbox/kuyruk **yok**; dış HTTP yalnız VIES (sabit URL); **SSRF koruması yok, pod egress'i açık** | API-9, API-10 |
| Sistem başına API anahtarı | Yok. Token kalıbı var (32 bayt, HMAC'li hash, redakte tip) ama paylaşılan yardımcı yok | API-3 |
| Idempotency-Key, `limit ≤ 1000`, JSON hata gövdesi, 429 + `Retry-After` | Hiçbiri yok; `httpx.Limiter` (bellekte, `replicas: 1`) var | API-4 |
| OpenAPI + sandbox | Yok. `TAPPA_DEV_TOOLS` dokunuş simülatörü **yalnız dev'de** | API-11, API-12, **K-3** |
| Bir telefon tek bir çalışana bağlı | **Var** — tarayıcı başına tek `tappa_session` çerezi | — |

**Eşleme notu — KM'nin "departmanları".** Spec'te KM-ERP departmanları
(`DEP-1`…) **lokasyon** olarak gönderir ve etiket onlara bağlanır. Tappa'da
plaket **lokasyona** bağlanır, departman lokasyonun alt birimidir. Dolayısıyla
spec'in Location'ı = Tappa `locations`; KM'nin her departmanı Tappa'da **bir
lokasyon** olur (bugünkü seed'de KM 1 lokasyon + 5 departman — prod
yapılandırması buna göre kurulur). Tappa `departments` v1 API'sinde görünmez.

---

## 2. Sözleşme farkları — entegratöre bildirilecek

Spec'in birebir karşılanamayan ya da Tappa kuralı gereği farklı davranan
noktaları. API-12'nin teslim ettiği rehberde bu liste **açıkça** yazılır.

1. *(K-2'ye bağlı)* Aktivasyon dokunuşu punch **değildir**; ilk punch ikinci dokunuştur.
2. `flag` verdict'li dokunuş yalnız **müdür onaylarsa** ve **onay anında** akışa girer
   (`punchTime` gerçek an kalır). Reddedilen hiç gelmez.
3. v1'de `version` hep `1`, `voided` üretilmez (Tappa'da geçerli kaydı iptal yolu
   yok). Alanlar sözleşmede durur; iptal yolu açılırsa kırıcı değişiklik olmaz.
4. Ek alan: `channel` (`nfc` | `qr`); QR punch'ında `tagId: null`.
5. Lokasyon `timezone`'u tenant'ın saat dilimine eşit olmalı (bugün `Europe/Malta`), değilse 422.
6. Pasif çalışanı yeniden aktifleştirmek (yeniden işe alım) **yeni `employee.id`**
   üretir; `externalRef` aynı kalır (ADR 0010 — deaktivasyon tek yönlü).
7. API ile açılan lokasyon panelden **GPS/IP** tanımlanana kadar dokunuşları
   onaya düşer (§5 satır 7) → onaylananlar sonradan gelir.
8. Çalışan e-postası aynı tenant'ın bir yönetici adresi olamaz (422); aynı kutuya
   davet tavanı → 429 + `Retry-After`.
9. Aktivasyon yalnız NFC ile (QR aktive edemez — ADR 0026 bedel 3; iPhone X ve öncesi aktive olamaz).
10. Webhook'larda sıra garantisi yok; tekilleştirme `id` + `version` ile (spec zaten böyle).
11. `recordedAt` = Tappa'nın kaydı yazdığı an. Onaylı flag'lerde akışa giriş anı daha geç olabilir.
12. Uzun süren bir veritabanı işlemi akışı **geciktirebilir**, punch **kaybettiremez** (K-13).
13. Manuel kayıtlar akışa girmez (K-6).

---

## 3. Kararlar

### Kullanıcıya sorulan (kırmızı çizgi / ürün sahibi kararı) — ✅ 2026-10-09

| # | Karar | Seçenekler | Karar |
|---|---|---|---|
| **K-1** | **Çapraz şirket dokunuşu** (spec §1 kuralı) — §4.5'e değer | (A) **Tek tenant, iki şirket**: `companies` tenant içi boyut; KM çalışanının KF şubesinde dokunması tenant içi, §4.5 değişmez · (B) **İki tenant + federasyon bağı**: §4.5'e ikinci bilinçli istisna (`transactions` RLS'ine işveren dalı, tap yolunda tenant-ötesi oturum/yön/debounce) · (C) v1'de çapraz dokunuş yok | ✅ **(A)** — kullanıcı |
| **K-2** | **Aktivasyon dokunuşu punch mı?** — ADR 0026 + §5 | (a) ADR 0026 kalır, spec metni düzeltilir · (b) ADR 0026 tadil: aktivasyon dokunuşu `Decide`'dan geçip kayıt yazar (butonsuz GET → GPS yok → IP yoksa `flag`) | ✅ **(a)** — kullanıcı |
| **K-3** | **Sandbox ortamı** — deploy/maliyet | (a) ayrı kurulum `sandbox.taptime.mt` (aynı k3s düğümü, ayrı namespace + Postgres, `TAPPA_DEV_TOOLS` açık → dokunuş simülasyonu) · (b) prod içinde test tenant'ı + `tt_test_` anahtar + simülasyon ucu · (c) sandbox yok | ✅ **(a)** — kullanıcı |

**K-1 (A)'nın bedeli:** panel her iki şirketi birlikte gösterir (aynı sahip);
tenant ayarları (politika, marka, saat dilimi) paylaşılır.
**Prod durumu (kullanıcı, 2026-10-09): KM tenant'ı prod'da VAR ama dokunuş
verisi YOK.** Dolayısıyla `transactions` taşıma sorunu yok: KM şirketi KF
tenant'ında açılır, KM lokasyon ve çalışanları **KM-ERP'nin kendi API
çağrılarıyla** gelir, KM çalışanları birleşik tenant'ta aktive olur, eski KM
tenant'ı arşivde kalır. ⚠️ **Tek açık nokta — KM'nin plaketleri:** `tags` satırı
hiçbir temizlikte silinmez (silinirse çip kalıcı kilitlenir) ve anahtarı KM
tenant'ının satırında. Seçenekler: (i) tek seferlik, audit'li bir migration ile
`tags.tenant_id` (+ `location_id`) taşınır — tenant-ötesi tek seferlik veri
hareketi, ayrı ADR notu ve güvenlik denetimi ister; (ii) plaketler yeni tenant'ta
yeniden encode edilir — Android rölesine (M8-05 B3, donanıma bloke) bağlı.
API-13 öncesi KM'nin prod plaket sayısı ölçülür ve bu seçim **kullanıcıya
sorulur** (§4.5'e değer).
**K-1 (B)'nin bedeli:** CLAUDE.md §4.5'in *"bilinçli TEK istisna"* cümlesi
değişir; yön zinciri ve 60 sn debounce kişi başına iki tenant'ı birden okumak
zorunda; hangi tenant'ın politikası uygulanır sorusu açılır. İşin en riskli
parçası olur, süre ~2–3×.

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe |
|---|---|---|
| K-4 | Akışa giren punch = `verdict='ok'` **veya** (`flag` ∧ `transaction_reviews.outcome='approved'`); `reject`/`ignored`/reddedilmiş flag **asla** | Spec §3.5 "yalnız geçerli olanlar"; §4.6 Tappa içinde karşılanır (kayıt durur, yalnız dışarı gitmez) |
| K-5 | v1'de iptal yolu yok → `version=1`, `status='valid'` | ADR 0011: düzeltme bağsız yeni satır; ADR 0009: onay geri alınamaz. Bir "void" ürünü ayrı karar |
| K-6 | `channel='manual'` ve tarihsel `practice=true` satırlar akışa girmez | Spec §9: manuel okutma RMS/ERP'nin; çift sayımı önler. Entegre şirkette manuel kayıt formuna uyarı satırı |
| K-7 | Lokasyon başına saat dilimi sütunu **eklenmez**; `timezone` tenant'ınkine eşit değilse 422 | Raporlar tenant saat dilimiyle hesaplıyor; ikinci saat dilimi kaynağı gece vardiyası hatası kaynağı (§6) |
| K-8 | Dış kimlikler: `emp_`/`loc_`/`pch_`/`dlv_` + UUID; `tag_` + uid | İç UUID zaten tahmin edilemez; önek tip karışıklığını yakalar |
| K-9 | Ayrı host `api.taptime.mt` (`TAPPA_API_HOST`, iki yönlü host kapısı — ops yüzeyi kalıbı) | Panel çerezleri (host-only) API host'una hiç gitmez; HTML rotaları API host'unda 404 |
| K-10 | Anahtar biçimi `tt_live_<43 base64url>` / `tt_test_…`; depoda `HMAC-SHA256(TAPPA_API_KEY_HMAC_KEY, token)`; kendi env anahtarı | Mevcut token kalıbı; önek sızıntı taramasını (GitHub secret scanning vb.) mümkün kılar; bağımsız kimlik = bağımsız anahtar (`adminauth/token.go:82-87` uyarısı) |
| K-11 | İmza spec §4.5'teki biçimde: imza başlığı = `sha256=` öneki + gövdenin HMAC-SHA256'sı (hex, uç noktanın paylaşılan anahtarıyla); ek olarak bir zaman damgası başlığı | Spec'e birebir uyum; tüketici `id+version` ile tekilleştirdiği için tekrar oynatma zararsız |
| K-12 | Webhook adresi ve sırrı **panelden** (owner) yönetilir, API'den değil; sır bir kez gösterilir, döndürülebilir | Spec §8: adresleri müşteri bize verir; API anahtarı sızarsa webhook'un yönlendirilmesini engeller |
| K-13 | İmleç = `(xid8, seq)`; yalnız `txid < pg_snapshot_xmin(pg_current_snapshot())` satırlar döner (Postgres 17 — ölçüldü) | Arka plan sıralayıcı işçisi gerekmez; geç commit eden işlem asla atlanmaz (ispat §4.4) |
| K-14 | `employees.location_id` API'den yönetilen satırlarda NULL olabilir: `CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL)`; manuel kayıt formu böyle bir çalışan için lokasyon seçtirir | Spec'te ev lokasyonu yok ("her lokasyonda okutabilir"). **API-1 yapıcısı önce `location_id`'nin bütün okuyucularını sayar;** sayı kabul edilemezse geri dönüş: opsiyonel `homeLocationRef` alanı |
| K-15 | E-posta değişince davet **yalnız aktive olmamış** çalışana yeniden gider | Spec'in amacı çalışana aktivasyonu ulaştırmak; aktive çalışana e-posta gürültü |
| K-16 | API'den yeni çalışan = otomatik davet (panelden eklemenin aksine) | Spec §4.2. Panel davranışı değişmez |
| K-17 | Davet **kuyruğa** girer, istekte gönderilmez: çalışana `invite_requested_at` yazılır; işçi tavanlar içinde **kodu gönderim anında basar** ve yollar. Yanıt: `activationEmail: queued\|sent\|blocked` | İlk toplu senkron (yüzlerce çalışan) senkron gönderimde tenant tavanına (50/saat) çarpar. ADR 0022 DB outbox'ı *link/kod saklayacağı için* reddetmişti — burada kuyrukta **kod yok**, yalnız "gönder" isteği var |
| K-18 | İlk bağlama: API `PUT /employees/{ref}` bilinmeyen ref için önce **aynı şirkette, `external_ref`'i boş, e-postası eşleşen** çalışanı bağlar (`employee.linked` audit); yoksa yeni satır açar. Lokasyonlarda otomatik eşleme yok — owner paneldeki "External ref" alanını doldurur | Pilot verisi (canlı KF çalışanları/plaketli şubeler) çift kayda dönüşmesin. Spec "isimle eşleşme yok" der; e-posta isim değildir ve tenant içi tekildir |

---

## 4. Tasarım özü (ADR 0027 ve 0028'de normatif olacak)

### 4.1 Şirket boyutu *(K-1 = A varsayımıyla)*

- `companies(tenant_id, id, code, name, vat_number NULL, created_at)` — RLS beşlisi;
  `UNIQUE(tenant_id, code)`. Her mevcut tenant'a migration'da **bir varsayılan
  şirket** (kod = tenant'tan türetilir); tek şirketli müşteri farkı hiç görmez.
- `locations.company_id`, `employees.company_id` — NOT NULL, varsayılanla doldurulur,
  bileşik FK `(company_id, tenant_id)`.
- Tap motoru **değişmez**: şirket karar girdisi değildir; dokunuş tenant içidir.
- Panel: şirket sütunu/filtresi listelerde; şirket yönetimi owner'da.

### 4.2 API anahtarı

- `api_keys(tenant_id, id, company_id, name, token_hash, prefix_hint, environment, created_by, created_at, last_used_at, revoked_at)` — RLS beşlisi; `token_hash` tekil.
- Kimlik: `Authorization: Bearer tt_live_…` → **yedinci resolver**
  `resolve_api_key_by_hash` (SECURITY DEFINER, `tappa_resolver`; ADR 0002 §7'nin
  beş şartı yeniden kazanılır) → `(tenant_id, key_id, company_id)`; sonraki her
  sorgu `WithTenant` + açık `tenant_id` filtresi (§4.5 kuşak+kemer).
- Yetki (spec §4 tablosu): yazma = anahtarın şirketinin çalışan/lokasyonu ·
  okuma = `scope=location` (şirketin lokasyonlarındaki tüm punch'lar) +
  `scope=employer` (şirketin çalışanlarının her yerdeki punch'ları) + rehber.
- Panel (owner): oluştur (bir kez göster) · listele (ad, önek, son kullanım) ·
  iptal. Audit: `api_key.created`, `api_key.revoked`. `last_used_at` dakikada en çok bir kez yazılır.
- Log: anahtar, hash'i ve `Authorization` başlığı **asla** (R7 tarayıcısına
  `tt_live_`/`tt_test_` kalıbı eklenir — R7d).

### 4.3 Punch = ne?

Dış dünyadaki punch, `transactions` satırının **dışarıya bakan izdüşümüdür**;
izdüşüm değiştiğinde (ilk yayın, ileride iptal) **append-only** bir olay yazılır:

- `punch_events(tenant_id, seq bigint identity, txid xid8 DEFAULT pg_current_xact_id(), punch_id → transactions, version, status, created_at)` —
  `transactions` gibi UPDATE/DELETE/TRUNCATE tetikleyicileri; `tappa_app` yalnız SELECT/INSERT.
- **Yazıldığı iki yer, ikisi de kaydın kendi DB işleminde:** (1) `checkin` kaydı
  `ok` ∧ kanal ≠ `manual` → `version=1`; (2) review `approved` → `version=1`.
  Saf `tap.Decide` ve `policy` **dokunulmaz** — yayın kayıt katmanında.
- **Geri doldurma:** migration'da geçmiş uygun satırlar için olay (sıra = `occurred_at`).

### 4.4 Akış imleci — neden kaybolmaz

Sorgu: `WHERE txid < pg_snapshot_xmin(pg_current_snapshot()) AND (txid, seq) > ($cursor) ORDER BY txid, seq LIMIT $n`.
`xmin` = hâlâ açık en eski işlemin xid'i. Döndürülen her satırın `txid`'i < `xmin`;
ileride yazılacak **her** satır ya hâlâ açık bir işlemden (xid ≥ `xmin`) ya da
henüz başlamamış bir işlemden (xid ≥ sıradaki xid ≥ `xmin`) gelir → hepsi imlecin
**ötesinde** sıralanır. `seq` tek başına yetmez: `seq=103` alıp geç commit eden
işlem, `seq=105`'i çoktan döndürmüş bir okuyucuyu atlatır (API-2'nin
eşzamanlılık testi bunu **kırmızıyla** kanıtlamalı). Bedel: uzun bir işlem akışı
bekletir — gecikme, kayıp değil (sözleşme farkı 12). İmleç opak (base64), içinde
yalnız `(txid, seq)`; tenant/kapsam filtresi her sorguda ayrıca.
Geçmiş sorgusu (`from`/`to`) `occurred_at` aralığıyla aynı yayın kuralını
`transactions` + `transaction_reviews` üstünden okur; aralık en çok 31 gün, sayfalı.

### 4.5 Webhook

- `webhook_endpoints(tenant_id, id, company_id, url, secret_sealed, active, created_at, rotated_at)`
  — sır `sun.Seal(TAPPA_WEBHOOK_KEK, AAD=endpoint id)` (operatör TOTP emsali; kendi KEK'i).
- `webhook_deliveries(tenant_id, id, endpoint_id, punch_event_seq, attempt, next_attempt_at, state pending|delivered|dead, last_status, last_error_class, created_at, delivered_at)`
  — `UNIQUE(endpoint_id, punch_event_seq)`.
- **Yönlendirme (spec §4.5):** olay → lokasyonun şirketinin uç noktası ∪ çalışanın
  işvereninin uç noktası (aynıysa tek).
- **İşçi (tek goroutine, süreç içi):** (1) yayılım: her uç noktanın imlecinden
  sonraki `punch_events`'i teslim satırına çevirir (K-13 imleciyle, aynı garanti);
  (2) gönderim: vadesi gelen teslimleri `FOR UPDATE SKIP LOCKED` ile alır (rollout
  sırasında iki pod — `maxSurge: 1` — çift göndermez); geri çekilme 1 dk · 5 dk ·
  30 dk · 2 sa · 6 sa → `dead`. Panelde teslim günlüğü + "yeniden gönder".
- **Tenant-ötesi tarama:** işçi vadesi gelen işleri bilmek için tenant'ları gezmek
  zorunda → resolver tarzı bir SECURITY DEFINER fonksiyon **yalnız `(tenant_id, delivery_id)` çifti** döndürür,
  yük her zaman `WithTenant` içinde okunur. ADR 0028'de §4.5 açısından gerekçelenir.
- **SSRF:** yalnız `https`, port 443, IP literali yok; **dial anında** çözülen adres
  `net.Dialer.Control` ile denetlenir (loopback, RFC1918, link-local/169.254, CGNAT,
  ULA, multicast reddedilir); yönlendirme takip edilmez; toplam 10 sn; yanıt
  gövdesi 64 KiB'te kesilir. Bugün egress NetworkPolicy yok — bu kapı tek savunma.
- **Kapanış bütçesi:** `srv.Shutdown 20 s + drain ≤ 3 s` < `terminationGracePeriodSeconds: 30`
  (`TestShutdownBudget_*`); işçi uçuştaki isteği iptal eder, teslim `pending` kalır.

### 4.6 Ortak sözleşme

JSON hata gövdesi `{code, message, details}`; 400/401/403/404/409/422/429/503;
429'da `Retry-After`. `limit` ≤ 1000 (varsayılan 500). İstek gövdesi ≤ 64 KiB.
`Idempotency-Key` (yazma isteklerinde opsiyonel): `api_idempotency(tenant_id, key_id, idem_key, request_hash, status, body, created_at)`,
24 sa; aynı anahtar + farklı gövde → 422. Anahtar başına oran sınırı (`httpx.Limiter`,
`replicas: 1` olduğu için yeterli — T1 notu geçerli). OpenAPI 3.1 **elle yazılır**,
`web/static` gibi embed edilir, `GET /v1/openapi.yaml` (Node yok, üretici yok).

### 4.7 Askıya alma (ADR 0025)

OP-15 bekliyor; canlıya girdiğinde askıdaki tenant'ın API anahtarları 403
`tenant_suspended` alır, webhook gönderimi durur (kuyruk korunur). API-4 bunun
için tek bir kontrol noktası bırakır. ⚠️ OP-15'in migration numarası şimdiden çakışıyor (00035); M11 migration'ları onu **beklemez**, OP-15 yeniden numaralanır.

---

## 5. Görevler

| ID | Görev | Boyut | Ajan | Bağımlılık |
|---|---|---|---|---|
| API-0 | ADR 0027 (şirket boyutu / K-1) + ADR 0028 (dış API: anahtar, punch yayını, imleç, webhook, SSRF) + ADR 0026 notu (K-2) | M | yapıcı + üçüncü göz | K-1, K-2 |
| API-1 | Şema: `companies`, `company_id`'ler, `external_ref`, `employee_code`, `first_name`/`last_name`, `employees.updated_at`, `locations.active`, K-14 CHECK; panelde "External ref" alanları | L | `tappa-db-migrator` | API-0 |
| API-2 | `punch_events` + iki yayın noktası + geri doldurma + **geç commit testi** + RLS testi | M | `tappa-db-migrator` | API-1 |
| API-3 | `api_keys` + `resolve_api_key_by_hash` + `internal/apikey` (redakte tip) + panel sayfası (owner) + audit | M | yapıcı (+ `tappa-brand`) | API-1 |
| API-4 | `/v1` iskeleti: host kapısı, Bearer ara katmanı, oran sınırı, JSON hata, gövde sınırı, Idempotency-Key, `openapi.yaml` iskeleti, askı noktası | M | yapıcı | API-3 |
| API-5 | `PUT /v1/locations/{ref}` (K-7, K-18) | S | yapıcı | API-4 |
| API-6 | `PUT/GET/DELETE /v1/employees/{ref}`, `resend-activation`; K-14…K-18; davet kuyruğu işçisi | L | yapıcı | API-4, **EM-5B (canlı için)** |
| API-7 | `GET /v1/punches` (iki kapsam, imleç, `from/to`) | M | yapıcı | API-2, API-4 |
| API-8 | `GET /v1/directory?updatedSince` | S | yapıcı | API-4 |
| API-9 | `webhook_endpoints` + mühürlü sır + SSRF-güvenli istemci + panel (adres, sır göster/döndür, test ping) | M | yapıcı (+ `tappa-brand`) | API-1 |
| API-10 | Webhook işçisi: yayılım, SKIP LOCKED, geri çekilme, `dead`, yeniden gönder, kapanış bütçesi | L | yapıcı | API-2, API-9 |
| API-11 | Sandbox (K-3) | M | yapıcı + kullanıcı (DNS/kurulum) | API-7, API-10 |
| API-12 | OpenAPI tamamı + entegrasyon rehberi (EN; §2 farkları dahil) | S | yapıcı | API-5…API-10 |
| API-13 | Canlıya alma: sırlar (`TAPPA_API_KEY_HMAC_KEY`, `TAPPA_WEBHOOK_KEK`), `api.` DNS + Ingress, **`tappa-security-auditor` tam tur**, KF/KM bağlama runbook'u (K-18), KM şirketinin KF tenant'ında açılması + KM plaketlerinin akıbeti (§3 K-1 notu — kullanıcıya sorulur), spec §7 kabul listesi | M | yapıcı + denetçi + kullanıcı | hepsi |

Her görev: yapıcı (opus) → **ayrı** üçüncü göz → bulgu varsa düzelt + yeniden
denetle (CLAUDE.md §10). Büyük testler (`-race` tamamı, `make check`) yalnız görev
sonunda bir kez. Dal: `m11-api` (`main`'e birleştirme = deploy kararı, kullanıcının).

### Kabul ölçütleri (görev kartına açılırken genişler — burada bağlayıcı çekirdek)

- **API-2:** iki eşzamanlı işlem, düşük `seq`'li olan **sonra** commit eder → okuyucu
  ikisini de alır (`seq`-yalnız imleçle aynı test **kırmızı** olmalı — mutasyon kanıtı).
  B tenant bağlantısı A'nın `punch_events`'ini görmez (filtresiz sorguyla).
  `flag` yazan dokunuş olay **yazmaz**; onaylanınca yazar; reddedilince yazmaz.
- **API-3:** iptal edilmiş anahtar → 401; anahtar log'da/hata metninde/audit'te yok
  (R7 kalıbı yeşil); B'nin anahtarı A'nın tek satırını okuyamaz/yazamaz (her uç için sonda).
- **API-4:** API host'unda `/admin` 404, ana host'ta `/v1` 404; aynı Idempotency-Key +
  farklı gövde 422; 429'da `Retry-After`.
- **API-6:** pasif çalışana `active:true` → yeni satır, eski geçmiş yerinde; aynı
  e-posta ile ikinci aktif çalışan 409; davet tavanı → `activationEmail: queued`, işçi
  tavan açılınca gönderir; kuyrukta **kod yok** (şema + log taraması).
- **API-7:** `scope=location` B şirketinin çalışanının A lokasyonundaki punch'ını
  döndürür, `scope=employer` A çalışanının B lokasyonundakini; manuel ve reddedilmiş
  satır hiçbir kapsamda yok.
- **API-10:** 169.254.169.254, `localhost`'a çözülen ad, `http://`, 302 yönlendirme
  reddedilir; aynı teslim iki pod'dan bir kez gider; imza spec'in örnek vektörüyle doğrulanır.

---

## 6. Riskler ve tuzaklar (ölçülmüş)

- **R7 log tarayıcısı** `token|secret|…` geçen her log çağrısını düşürür — webhook/anahtar kodunda adlandırma buna göre.
- **Tek replika, bellekte limitler:** oran sınırı ve e-posta kesicisi süreç başına; rollout'ta 2× patlama mümkün (T1).
- **E-posta prod'da kapalı:** API-6 canlıda EM-5B olmadan yalnız `activationEmail: blocked` döner. EM-5B M11'in canlı ön koşuludur.
- **Davet tavanları toplu senkronla çakışır** → K-17 kuyruğu; 300/saat süreç kesicisi ilk senkronu saatlere yayabilir — runbook'ta yazılır.
- **Pilot verisinin çiftlenmesi** → K-18; API-13 runbook'u ilk `PUT`'tan **önce** panel eşlemesini ister.
- **GDPR:** rehber iki şirketin çalışan adlarını karşılıklı açar (spec istiyor); DPA/aydınlatma metni güncellemesi kullanıcıda (Q13 ile birlikte).
- **Egress açık:** SSRF kapısı tek savunma; NetworkPolicy egress'i ayrı bir sertleştirme (backlog adayı).

## 7. Kullanıcının dış adımları

1. ~~K-1…K-3 kararları~~ ✅ 2026-10-09.
2. `api.taptime.mt` ve `sandbox.taptime.mt` DNS kayıtları — Cloudflare, DNS-only.
3. KF-RMS / KM-ERP ekibinden canlı + test webhook adresleri (spec §8).
4. Canlı öncesi: paneldeki KF lokasyonlarına `externalRef` girilmesi (K-18).
5. KM plaketlerinin akıbeti (taşıma mı, yeniden encode mu) — API-13 öncesi.

## 8. Kapsam dışı (v1)

Punch yazma API'si (spec §9 gerekmez diyor) · punch iptali/düzeltmesi · departman
uçları · lokasyon silme · webhook yönetimi API'den · lokasyon başına saat dilimi ·
OAuth/istemci kimliği · çoklu replika için paylaşımlı oran deposu.
