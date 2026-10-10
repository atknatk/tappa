# M11 — Entegrasyon API'si (KF-RMS · KM-ERP)

> **Durum:** PLAN, **6. sürüm — MODEL DEĞİŞTİ** (2026-10-10, 28. oturum). Kod yok.
> 5. sürüm (`690c69b`) iki mercekten ONAY almıştı, ama **"tek tenant, iki şirket"**
> modeline dayanıyordu. Kullanıcı onaydan sonra modeli değiştirdi: **KF ve KM aynı
> holding altında ayrı iki tenant kalır; holding bağı çapraz dokunuşa izin verir**
> (K-1, §3). Bu sürüm holding modelini kod üstünde çıkarılmış bir etki haritasına
> dayanarak yazar. Önceki onaylar **değişmeyen** parçaları (API anahtarı, imleç, olay
> yazıcısı, webhook, SSRF, Idempotency, sandbox derlemesi) kapsar; holding parçaları
> (§4.1, §4.3–§4.4'ün iki-tenant hâli, API-1b…API-1d) **yeni tam denetim** ister.
> Sıradaki adım: 6. tur denetim (iki mercek), ONAY gelirse API-0.
>
> **Kaynak:** kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve
> KM-ERP entegrasyonu)"*, 2026-10-09. **Repoya konmadı** — depo public ve doküman
> müşterinin iç webhook adresini taşıyor. "spec §N" o dokümanın bölümüdür.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"**; çıplak "§N" bu planın bölümüdür.

**Amaç.** Tenant'ın panelinden ürettiği **API anahtarlarıyla** dış İK/ERP sistemlerinin
(ilk müşteri: KF-RMS → KF tenant'ı, KM-ERP → KM tenant'ı) Tappa'ya lokasyon ve çalışan
göndermesi, punch'ları **imleçli akıştan** çekmesi ve **webhook** ile anında alması; ve
aynı holding'deki tenant'ların çalışanlarının birbirinin plaketlerinde okutabilmesi.

**Neden ayrı milestone.** Bugün dış dünyaya açık hiçbir JSON API, Bearer kimliği, webhook,
OpenAPI ya da Idempotency-Key yok; tenant'lar arası dokunuş `sys:tenant-mismatch` ile
reddediliyor (ölçüldü). Bu iş yeni bir **güvenlik sınırı**, yeni bir **giden ağ yüzeyi**,
yeni bir **sıralama sözleşmesi** ve CLAUDE.md §4.5'e **ikinci bilinçli istisna** açar —
hepsi ADR ister (CLAUDE.md §10).

---

## 1. Spec ↔ Tappa — bugünkü durum (ölçüldü)

| Spec beklentisi | Tappa bugün | Plan |
|---|---|---|
| İki şirket (KF/KM), **her çalışan iki şirketin her lokasyonunda okutabilir** | KF ve KM **iki ayrı tenant**; gruplama yok. Yabancı tenant plaketi → `sys:tenant-mismatch` → **403, kayıt yok** (`internal/policy/guardrails.go:306-319`, `internal/handler/checkin.go:227-233`) | **K-1** holding bağı (§4.1) |
| Sistem başına API anahtarı | Yok. Token kalıbı var (32 bayt, HMAC'li hash, redakte tip) | **Anahtar = tenant** (API-3) |
| Location: `externalRef`, `timezone`, `active` | external id yok · saat dilimi **tenant** sütunu · aktif bayrağı yok, silme sert `DELETE` (`db/queries/locations.sql:369`) | API-1, K-7, K-20 |
| Tag panelden lokasyona bağlanır | Var (`tags.uid` küresel PK). QR satırları da `tag_uid` taşır (`internal/domain/checkin/checkin.go:1282-1285`) | `tagId = tag_<uid>` |
| Employee: `externalRef`, `employeeCode`, `firstName`, `lastName`, `email`, `active`, `activated` | external id/kod yok · tek `full_name` · `email citext`, `employees_tenant_email_key (tenant_id, email) WHERE email IS NOT NULL` — **pasif satırlar dahil**; kod çakışmayı kısıt adıyla tanıyor (`internal/domain/tenant/staff.go:706-717`, `db/queries/employees.sql:783-784`) · `status` + `activated_at` var · `location_id NOT NULL` · `updated_at` yok | API-1, K-14, K-21, K-24 |
| Yeni çalışana **otomatik** aktivasyon e-postası | Çalışan eklemek davet göndermez; davet e-postası senkron. Tavanlar: tenant 50/saat 300/gün · çalışan 3/saat · kutu 5/saat 20/gün · süreç kesicisi 300/saat. **EM-5B canlıda** (`main` = `77cfb98`) | API-6, K-16, K-17 |
| **İlk dokunuş = ilk punch** | ADR 0026: aktivasyon dokunuşu `transactions` satırı yazmaz | **K-2** |
| "Doğrulanamayan dokunuş punch'a dönüşmez" | `flag` kaydı yazılır, onay kuyruğuna düşer (CLAUDE.md §4.6); onay `transaction_reviews` | K-4 |
| Düzeltme/iptal → yeni `version` | Geçerli kaydı iptal yolu yok (ADR 0011) | K-5 |
| Manuel punch'lar RMS/ERP'de (spec §9) | Tappa'da `channel='manual'` kayıt var | K-6 |
| İmleçli akış, **kayıt sırasına göre**, geç gelen kaçmaz | `transactions`'ta monoton anahtar yok | API-2, K-13 |
| Webhook + imza + tekrar deneme | DB outbox yok; dış HTTP yalnız VIES; dial-time SSRF koruması yok, pod egress'i açık | API-2, API-9, API-10, K-19 |
| Idempotency-Key, `limit ≤ 1000`, JSON hata gövdesi | Yok; `httpx.Limiter` ve 429'da `Retry-After` var (`internal/httpx/ratelimit.go:551`) | API-4 |
| OpenAPI + sandbox | Yok. Dokunuş simülatörü dört katlı kapının arkasında (`internal/config/config.go:731-745`, `internal/handler/devtap.go:71-78`). Oturum çerezleri `__Host-` önekli değil; `taptime.mt` altına güvenilmeyen alt alan eklenmesini yasaklayan kalıcı kısıt var (`internal/handler/cookies.go:138-159`) | API-11, API-12, **K-3**, **COOKIE-1** |
| Bir telefon tek bir çalışana bağlı | Var — tarayıcı başına tek `tappa_session` çerezi | — |

**Eşleme notu — KM'nin "departmanları".** Spec'te KM-ERP departmanları (`DEP-1`…)
**lokasyon** olarak gönderir ve etiket onlara bağlanır. Tappa'da plaket lokasyona bağlanır;
KM'nin her departmanı KM tenant'ında **bir lokasyon** olur.

---

## 2. Sözleşme farkları — entegratöre bildirilecek

API-12'nin teslim ettiği rehberde bu liste **açıkça** yazılır.

1. Aktivasyon dokunuşu punch **değildir**; ilk punch ikinci dokunuştur (K-2). Aktivasyon yalnız **kendi şirketinin** bir plaketinde tamamlanır (K-1b).
2. `flag` verdict'li dokunuş yalnız **işveren şirketin yöneticisi onaylarsa**, **onay anında** akışa girer; reddedilen hiç gelmez (K-4, K-1a).
3. v1'de `version` hep `1`, `voided` üretilmez (K-5).
4. Ek alan: `channel` (`nfc` | `qr`). Her iki kanalda da `tagId` doludur.
5. `punchTime` = telefonun **beyan ettiği** an. Çevrimdışı kuyruk **varsayılan** 120 sn'ye kadar `ok` kalır (tenant'ça ayarlanabilir baseline — `internal/policy/baseline.go:109-118`); daha eskisi onaya düşer; tavan 72 sa. `recordedAt` = sunucunun kaydı yazdığı an.
6. 60 sn içindeki tekrar dokunuş (`ignored`) punch **değildir** — kişi bazlı, **iki şirketin plaketleri arasında da** geçerli.
7. Lokasyon `timezone`'u tenant'ın saat dilimine eşit olmalı (bugün `Europe/Malta`), değilse 422 (K-7).
8. Lokasyonda `active:false` yalnız etikettir; dokunuş kararını değiştirmez (K-20).
9. Yeniden işe alım (pasif → `active:true`) **yeni `employee.id`** üretir; `externalRef` aynı kalır (ADR 0010).
10. API ile açılan lokasyon panelden **GPS/IP** tanımlanana kadar dokunuşları onaya düşer.
11. Çalışan e-postası ASCII olmalı, aynı tenant'ın bir yönetici adresi olamaz (422); tenant içinde aktif başka bir çalışanda kullanılıyorsa 409 `email_unavailable`. Davet her zaman **kuyruğa** girer (`activationEmail: queued`) (K-17).
12. `resend-activation`: kuyrukta bekleyen ya da son bir saatte gönderilmiş canlı bir davet varken **etkisizdir** (202 `already_queued` / `recently_sent`).
13. Aktive olmuş çalışana `resend-activation` → 409 `already_activated`.
14. Aktive olmuş çalışanın e-postası değişirse **davet gitmez** (spec §4.2'den sapma — K-15).
15. Aktivasyon yalnız NFC ile (QR aktive edemez; iPhone X ve öncesi aktive olamaz).
16. Webhook'ta sıra garantisi yok; tekilleştirme `id` + `version` ile.
17. Manuel kayıtlar akışa girmez (K-6).
18. Geçmiş sorgusu (`from`/`to`) en çok 31 gün, sayfalı.
19. `updatedSince` (rehber) **10 dakikalık örtüşme penceresiyle** çalışır; tekrar dönenler `id` ile ezilir (K-21).
20. Sunucu yeniden başladığında (her deploy dahil) bekleyen webhook tekrar denemeleri ve kuyruktaki davetler, o tenant'ın bir sonraki dokunuşuna ya da API çağrısına kadar bekler (K-19). İmleçli akış bundan etkilenmez.
21. Panelden eklenmiş ve henüz bağlanmamış bir çalışanın punch'ı `employee.externalRef: null` ile gelir (K-18 runbook'u canlı öncesi bağlar).
22. Pasifleştirme anahtar başına **saatlik bütçeyle** sınırlıdır; aşılınca 429 (K-25).
23. API ve webhook, operatörün tenant için açtığı erişimle çalışır (K-22).
24. Ev lokasyonu olmayan (API'den yönetilen) çalışanın punch'ına "farklı şubede dokundu" notu düşmez (`internal/domain/tap/decide.go:133-134`) — bilgi zaten `location` alanında (K-14).
25. **Şirketler arası dokunuşun kararı işverenin kurallarıyla verilir** (K-1a): bir KF şubesine özel politika istisnası o şubede okutan KM çalışanına **uygulanmaz**.
26. **Holding üyeliği bittiğinde** (operatör çıkarır) çapraz dokunuş durur; 5 dakikalık geçiş penceresinde karara girmiş dokunuşlar yine yazılır (§4.1).

---

## 3. Kararlar

### Kullanıcı kararları

| # | Karar | Seçilen | Bedeli (kayıtlı) |
|---|---|---|---|
| **K-1** | Şirketler arası dokunuş (CLAUDE.md §4.5) | **KF ve KM aynı holding altında ayrı iki tenant; holding bağıyla çapraz dokunuş** (2026-10-10; 5. sürümün "tek tenant, iki şirket" modelinin **yerine**) | CLAUDE.md §4.5'e **ikinci, adı konmuş bilinçli istisna** (yalnız holding içi); ADR 0002/0004/0007/0016/0021/0023/0026 notları; tap yolunda iki dar tenant-ötesi nokta (§4.1). **Kazanç:** API anahtarı = tenant (kullanıcının ilk isteği), EM-12 ön koşul değil, KM plaketleri yerinde, markalar ayrı |
| **K-1a** | Çapraz dokunuşun kaydı kimin? | **İşverenin (KM)**: KM'nin politikası karar verir, onayı KM yöneticisi verir, manuel düzeltme KM'de; KF salt okunur görür | KF'nin lokasyona özel politika istisnaları KM çalışanına uygulanmaz (§2 madde 25) |
| **K-1b** | Aktivasyon hangi plakette? | **Yalnız kendi tenant'ının plaketi** (ADR 0026 değişmez) | Yalnız KF şubelerinde çalışan bir KM çalışanı da ilk kez bir KM plaketine dokunmalı |
| **K-1c** | KM çalışanı KF plaketinde — tap ekranı | **KF markasıyla** (logo + accent; sonuç ekranında logo) | CLAUDE.md §9 / ADR 0023'e not: holding plaketinde marka **plaketin** tenant'ınındır; marka okuması holding definer'ından gelir |
| **K-1d** | Holding içi görünürlük | **Kabul, DPA notuyla**: KF, plaketlerine dokunan KM çalışanlarının adını/kodunu ve dokunuş zamanını görür; KM, KF mekan adlarını görür; rehber iki tenant'ın adlarını (e-postasız) açar. GPS, IP, not, policy bağlamı **asla** karşı tarafa açılmaz | DPA / aydınlatma metni güncellemesi kullanıcıda |
| **K-2** | Aktivasyon dokunuşu punch mı? | **ADR 0026 kalır** (2026-10-09) | §2 madde 1 |
| **K-3** | Sandbox | **Alt alan** (`sandbox.taptime.mt`) + **ayrı derleme**; **önce `__Host-` çerez geçişi** (COOKIE-1) (2026-10-10; "ayrı alan adı"nın yerine) | COOKIE-1 sandbox'tan önce biter; geçiş kimseyi oturumdan düşürmeden yapılır (yapıcı ölçer) |
| **K-19** | Arka plan işçisi tenant'lar arası tarama yapsın mı? | **Etkinlikle kurulan işçi — tarama YOK** (2026-10-09) | §2 madde 20 |
| **K-25** | API'den pasifleştirme ADR 0010'un iki adımlı onayını atlar | **Anında + anahtar başına saatlik bütçe** (2026-10-10) | §4.9 |
| **K-26** | Askıda savunma yazmaları | **Açık kalır**: anahtar iptali, uç nokta devre dışı bırakma (2026-10-10) | §4.7 |

**Geçersiz kalan (5. sürüm):** `companies` boyutu, `company_id`'ler, şirket başına davet
tavanı, ikinci şirketi açan `op_*`, KM plaketlerinin taşınması, KM şirketinin açılması,
EM-12 ön koşulu, eski KM tenant'ının kapatılması. KM tenant'ı **olduğu gibi** kalır.

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe |
|---|---|---|
| K-4 | Akışa giren punch = `verdict='ok'` ∧ kanal ∈ {`nfc`,`qr`} ∧ ¬`practice`, **veya** aynı kanal/practice koşulunu sağlayan **onaylanmış** `flag`. Başka hiçbir şey | Spec §3.5; CLAUDE.md §4.6 Tappa içinde karşılanır (kayıt durur, yalnız dışarı gitmez) |
| K-5 | v1'de iptal yolu yok → `version=1`, `status='valid'` | ADR 0011, 0009 |
| K-6 | Manuel ve tarihsel practice satırlar akışa girmez | Spec §9 |
| K-7 | Lokasyon başına saat dilimi sütunu yok; farklıysa 422. Holding'deki iki tenant'ın saat dilimi **eşit olmalı** (üyelik ekleyen `op_*` reddeder) | Raporlar tenant saat dilimiyle (CLAUDE.md §6); çapraz vardiya yorumu tek saat dilimi ister |
| K-8 | Dış kimlikler: `emp_`/`loc_`/`pch_`/`dlv_` + UUID; `tag_` + uid | Önek tip karışıklığını yakalar |
| K-9 | Ayrı host `api.taptime.mt` (`TAPPA_API_HOST`), iki yönlü host kapısı | Panel çerezleri API host'una gitmez |
| K-10 | Anahtar: `tt_live_` öneki + 43 karakter base64url (sandbox'ta `tt_test_`); depoda kendi env anahtarıyla HMAC-SHA256; yanlış önek DB'ye gitmeden 401 | Mevcut token kalıbı; önek sızıntı taramasını mümkün kılar |
| K-11 | İmza spec §4.5 biçiminde (`sha256=` öneki + gövdenin hex HMAC-SHA256'sı); ek zaman damgası başlığı yok | İmzaya girmeyen başlık sahte güven verir |
| K-12 | Uç nokta adresi ve imza anahtarı panelden (owner); anahtar yalnız oluşturma/döndürmede bir kez gösterilir | "Göster" ele geçirilmiş oturuma sahte imzalı punch yollatırdı |
| K-13 | **İmleç = hedef tenant başına sayaç satırı** (`feed_counters`), tetikleyici içinde `INSERT … ON CONFLICT (tenant_id) DO UPDATE … RETURNING`; satır kilidi commit'e kadar. Holding punch'ı **iki hedef tenant'ın** sayacını kilitler → kilitler **kanonik sırada** (tenant id'ye göre artan) alınır | 3. turda ölçüldü (tek tenant): 0 kaçan, iki mutasyon kırmızı. İki-sayaç hâli yeni: KM@KF ile KF@KM ters sırada kilitlese kilitlenirdi — sıra + stres testi (API-2) |
| K-14 | `employees.location_id` API'den yönetilen satırlarda NULL olabilir (`CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL)`); manuel kayıt formu lokasyon seçtirir. **API-1 önce bütün okuyucuları sayar** | Spec'te ev lokasyonu yok; bedel §2 madde 24 |
| K-15 | E-posta değişince bekleyen davetler aynı işlemde emekliye ayrılır + audit; aktive değilse yeni davet kuyruğa | Eski kutudaki kodun çalışanı devralmasını önler |
| K-16 | API'den yeni çalışan = otomatik davet | Spec §4.2 |
| K-17 | Davet kuyruğu `invite_requests(tenant_id, employee_id, requested_at, lease_until, attempts)`; işçi kodu **gönderim anında basar**, eski davetleri aynı işlemde emekliye ayırır; kira 60 sn; `resend-activation` istek anında hiçbir daveti öldürmez. Tavanlar **bugünkü tenant tavanları** (panel ve kuyruk aynı sayaç) | Ayrı tenant'lar olduğu için KM'nin seli KF'nin tavanını zaten tüketemez; süreç kesicisi bugünkü gibi tenant başına en çok 50/300 |
| K-18 | İlk bağlama: bilinmeyen ref için önce **aynı tenant'ta**, pasif olmayan, `external_ref`'i boş, e-postası eşleşen çalışan bağlanır (`employee.linked` audit); lokasyonlarda owner panelden "External ref" girer | Pilot verisi çift kayda dönüşmesin |
| K-20 | Lokasyon `active` yalnız etiket | Kararı etkilemesi CLAUDE.md §5 + ADR ister |
| K-21 | `employees.updated_at` tetikleyiciyle, yalnız rehbere görünen sütunlar değişince; rehber `updatedSince − 10 dk` + `asOf` | Dört UPDATE yolu var; elle güncellemek birini unutur |
| K-22 | API ve webhook bir tenant için **yalnız operatör açınca** (`tenants.api_enabled_at`, açan/kapatan `op_*`); kapı her yolda (anahtar çözümü, uç nokta, ping, yeniden gönderim, teslim satırı, gönderim anı) | `/signup` herkese açık |
| K-23 | API kaynaklı audit: `actor_id = api_keys.id`, `detail.via = "api"` | |
| K-24 | E-posta tekilliği tenant içinde, **pasif satırlar hariç**: `(tenant_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'`, **kısıt adı korunur**. Holding genelinde tekillik **aranmaz** | Tenant içi tekillik bugünkü kural; holding genelinde aramak yeni bir tenant-ötesi okuma ve KF adresleri için KM'ye kehanet olurdu. **Artık risk (adlandırılmış):** aynı kişiyi iki tenant da davet edebilir; telefon tek oturum taşıdığı için son tamamlanan aktivasyon kazanır — bugün tenant'lar arası kabul edilmiş davet riskinin (EM-5B (c)) aynısı, ama aktivasyon K-1b gereği **o tenant'ın plaketinde fiziksel dokunuş** ister |
| K-28 | **`IsHardened()` = `prod` ∨ `sandbox`**; üretimdeki dokuz `IsProd()` çağrısının dokuzu da geçer: `internal/config/config.go:326`, `config.go:1000`, `internal/db/pool.go:119`, `internal/session/cookie.go:136`, `internal/adminauth/cookie.go:109`, `internal/handler/cookies.go:270`, `signupstate.go:440`, `logincontext.go:297`, `activate.go:184`. `EnvDev` kapıları sandbox'ta kapalı kalır | Sandbox internete açık |
| K-29 | **Holding üyeliği** tenant kapsamsız `holdings` + `holding_members(member_tenant_id UNIQUE)`; yalnız `op_*` yazar; çıkarma **5 dakikalık geçiş** (`left_at` = şimdi + 5 dk) ile | Karar ile kayıt arasında üyelik düşerse sayaç harcanmış, kayıt reddedilmiş olurdu (CLAUDE.md §4.6) |
| K-30 | Çapraz dokunuşun kaydı **işverende** (K-1a) ve plaketin tenant'ı ayrı `site_*` sütunlarında; `transactions` ve `transaction_reviews` **RLS'i değişmez**; KF görünürlüğü yazma anında üretilen bir **izdüşümden** | Kayıt plakette yazılsaydı 60 sn ve yön zinciri iki tenant'ı okumak zorunda kalırdı ve KF'de giriş + KM'de çıkış yanlış eşlenirdi; RLS'e dal eklemek satırın tamamını (GPS, IP, policy bağlamı) açardı |

---

## 4. Tasarım özü (ADR 0027 ve 0028'de normatif olacak)

### 4.0 Ortak şema kuralı — `arwd` varsayılanına güvenilmez

Üretimde `pg_default_acl` = `tappa_app=arwd/tappa_owner` (`deploy/README.md:5722-5746`);
taze CI veritabanında `ar`. Bu yüzden M11'in **her** yeni tablosunda `REVOKE ALL … FROM
tappa_app`, ardından yalnız gereken fiillere ve **sütunlara** açık `GRANT`; UPDATE hep sütun
listesiyle. **Tablo × fiil × sütun matrisi** ADR 0027/0028'de normatif; en az: `api_keys`
UPDATE yalnız `(revoked_at, last_used_at)`, iptal tek yönlü (00011 `tappa_forbid_revocation_reset`
emsali) ve her `revoked_at` yazması `COALESCE(revoked_at, now())` ve/veya `WHERE revoked_at IS
NULL` (00011 BOUNDARY 2, `:534-547`); `webhook_endpoints` UPDATE `tenant_id` içermez;
`feed_counters` — `tappa_app` yalnız SELECT. **Fonksiyonlar:** M11'in yarattığı her fonksiyonda
`REVOKE ALL ON FUNCTION … FROM PUBLIC` (yeni fonksiyon PUBLIC EXECUTE ile doğar — 4. turda
geçici tablo yolu ölçüldü). **Tenant kapsamsız tablolar** (`holdings`, `holding_members`):
R5 muafiyetiyle görünür, `tappa_app`'ten açık `REVOKE ALL`, ENABLE + FORCE RLS birlikte,
tenant verisi olmayan sütunun adı `tenant_id` **olmaz** (CLAUDE.md §6). **Kabul:** CI,
migration'lardan önce prod'un `arwd` varsayılanını kurar; **genel katalog testi** M11 tablo ve
fonksiyon listesini dolaşıp dört fiili ve EXECUTE ACL'ini matrisle karşılaştırır.

### 4.1 Holding bağı ve çapraz dokunuş (K-1, K-1a…d, K-29, K-30)

**Varlık.**
- `holdings(id, name, created_at)` ve `holding_members(holding_id, member_tenant_id UNIQUE,
  joined_at, left_at)` — tenant kapsamsız (§4.0). `UNIQUE(member_tenant_id)` → "eş tenant"
  geçişli. `left_at` geri alınamaz (00011 emsali tetikleyici).
- Yazarlar yalnız `op_create_holding`, `op_add_holding_member`, `op_remove_holding_member`
  (ADR 0021 sınıfı). Üyelik ekleme iki tenant'ın saat dilimi eşit değilse reddeder (K-7).
  Audit: `operator_audit_log` **ve** iki tenant'ın `audit_log`'u, aynı işlemde.
- Çıkarma `left_at = now() + 5 dk` yazar; tap yolu `left_at > now()`'u üye sayar (K-29).

**Rol.** Yeni **NOLOGIN, BYPASSRLS, üyesiz** `tappa_holdingdefiner` (`tappa_resolver` /
`tappa_opdefiner` kalıbı): `scripts/db-init/01-roles.sql`'in idempotent bloğunda + canlı küme
runbook'u; geri yükleme provası sıfırdan kurulmuş pod'a. Sahip olduğu her fonksiyon: sabit
`search_path = pg_catalog, pg_temp`, nitelikli adlar, dinamik SQL yok, sabit dönüş sütunları,
`REVOKE ALL … FROM PUBLIC` + yalnız `tappa_app`'e EXECUTE. **Çağıranın tenant'ı parametre
değildir, GUC'tan okunur**; girdi yalnız sunucunun çözdüğü anahtardır (uid ya da id); eş olmayan
tenant için **0 satır** (bilinmeyen anahtarla aynı cevap). Gövdede RLS yoktur → açık holding +
tenant filtresi tek bariyerdir (ADR 0021 §3.2'nin aynısı) → her fonksiyon için **holding dışı
üçüncü tenant C**'den 0 satır testi.

**Fonksiyonlar (holding istisnasının tamamı bunlar + §4.3 izdüşüm tetikleyicisi):**
| Fonksiyon | Ne döner / ne yapar | Çağrı yeri |
|---|---|---|
| `holding_tap_site(p_uid)` | eş tenant'ın plaketi için: `site_tenant_id`, `location_id`, mekan adı, `static_ips`, GPS, vardiya + overnight, saat dilimi; **marka** (logo kimliği + accent — K-1c, yalnız WL-13'ün VIES kapısını geçen tenant için logo) | `GET /t` sayfası, `checkin.gather` |
| `holding_advance_tag_counter(p_uid, p_ctr)` | **tek ifade**: `UPDATE tags SET last_ctr = $ctr WHERE uid = $uid AND last_ctr < $ctr AND <eş şartı aynı WHERE'de> RETURNING …` (`db/queries/tags.sql:33-46` kalıbı; Go'da okuma-karşılaştırma yasak — CLAUDE.md §4.4) | `sun.Verify`'ın holding kolu |
| `holding_directory(p_updated_since)` | holding'deki tenant'ların çalışanları: kimlik, tenant, `external_ref`, kod, ad, soyad, aktif — **e-posta yok** | `GET /v1/directory` |
| `holding_site_names(p_location_ids)` | KM raporları için KF mekan adları | KM panel raporları, anomali, onay kuyruğu |

**Kayıt (K-30).** `transactions`'a nullable `site_tenant_id`, `site_location_id`,
`site_tag_uid` (nullable sütun eklemek satır yeniden yazmaz — CLAUDE.md §4.3 korunur). FK
`(site_location_id, site_tenant_id) → locations(id, tenant_id)`, `(site_tag_uid, site_tenant_id)
→ tags(uid, tenant_id)`. CHECK: ya üçü de NULL, ya üçü dolu ∧ `site_tenant_id <> tenant_id` ∧
`location_id IS NULL` ∧ `tag_uid IS NULL`. Kısmi indeks `(site_tenant_id, occurred_at DESC)`.
`BEFORE INSERT` tetikleyicisi (holding definer'ı üzerinden) `site_tenant_id`'nin satırın
tenant'ıyla **aynı holding'de** olduğunu doğrular (geçiş penceresi dahil). Geriye doldurma yok.

**Tap yolu (etki haritasının P1–P12'sine göre).**
- `GET /t`: oturum KM, plaket KF → `holding_tap_site(uid)` 1 satır dönerse sayfa **KF mekan
  adı ve KF markasıyla** çizilir (K-1c); 0 satır → bugünkü davranış (adsız, sonra 403).
- `POST /api/checkin` → `advance`: plaketin tenant'ı oturumunkinden farklıysa ve eşse
  `holding_advance_tag_counter` (aynı işlem sınırı, tek ifade); değilse bugünkü gibi
  ilerletmeden reddet. **Eşzamanlılık testi:** aynı `(tag, ctr)` ile N goroutine, yarısı aynı
  tenant yolundan yarısı holding yolundan → **tam 1** başarı (CLAUDE.md §8).
- `gather`: çalışan, departman vardiyası, son kayıt, debounce, son açık giriş **KM'de, bugünkü
  sorgularla** (değişmez — kişinin bütün satırları KM'de). Yalnız **dokunulan lokasyonun
  kanıtı** (IP, GPS, vardiya, saat dilimi) `holding_tap_site`'tan.
- `tap.Decide`: yeni sunucu olgusu `SameHolding` (ADR 0004 §8: sunucuda türetilir, istemciden
  gelmez). Guardrail #1 `sys:tenant-mismatch` = `TagTenantID ≠ SessionTenantID ∧ ¬SameHolding`.
  #1'in uyarı bastırma gerekçesi yalnız holding dışı çiftler için geçerli kalır (ADR 0007 notu).
- Politika kümesi **KM'nin** (K-1a); `policy_version_fk` satırın tenant'ıyla tutarlı kalır.
- `write`: satır KM'de, `site_*` dolu, `location_id`/`tag_uid` boş; departman KM'nin.
- Deaktive çalışan uyarısı KM'nin audit'inde (bugünkü gibi). KF plaketinin **kayıp/emekli**
  reddi KF'ye **izdüşümden** görünür (`matched_sid = sys:tag-not-active`); çapraz audit yazılmaz.
- Sonuç ekranı: KF logosu (K-1c; bugün farklı tenant'ta logo yok — `internal/handler/checkin.go:320-323`).
- Aktivasyon: **değişmez** (K-1b) — `CompleteByTap`'in tenant eşitliği (`internal/handler/activate.go:797,828`) korunur.

**KF görünürlüğü — izdüşüm.** KF tenant'ında RLS beşlisiyle doğan, append-only
`site_punches(tenant_id = KF, transaction_id, employer_tenant_id, employee_id, employee_ref,
employee_code, employee_name, site_location_id, site_tag_uid, occurred_at, channel, verdict,
matched_sid, type, review_outcome, created_at)` — **GPS, IP, not, policy bağlamı yok**
(K-1d, CLAUDE.md §4.7). Satırları `AFTER INSERT ON transactions` / `ON transaction_reviews`
tetikleyicisi (holding definer sahipli) yazar: holding dokunuşu, **KF sitesinde açılmış bir
girişi kapatan çıkış** (işverendeki son açık girişin `site_tenant_id`'sinden) ve onay olayı.
KF'nin bütün okumaları sıradan, RLS'li, açık `tenant_id` filtreli sorgulardır.

**KF ve KM panelleri.**
- KF: "Holding dokunuşları" görünümü, işgücü raporu ("girişi KF sitesinde olan aralıklar"),
  plaketin son görülmesi ve sayaç boşlukları izdüşümden. KF **onay vermez** (K-1a), sonucu görür.
- KM: raporlar, açık kayıt anomalisi, liste ve onay kuyruğunda KF mekan adları
  `holding_site_names`'ten; KF sitesindeki vardiya KM bordrosunda "KF — <mekan>" kovasında.
- **Lokasyon silme:** KF'nin `CountLocationReferences`'ı (`db/queries/locations.sql:293-317`)
  KM'deki `site_location_id` referanslarını göremez → sayım izdüşümden de yapılır; FK RESTRICT
  son kemer.
- Manuel kayıt yalnız işverende; KF sitesindeki bir vardiyanın manuel düzeltmesi isteğe bağlı
  `site_*` alır ve aynı tetikleyiciden geçer.

**Değişen kurallar (API-0'da yazılır).** CLAUDE.md §4.5: *"Bilinçli istisnalar iki tanedir:
(1) `op_*` (ADR 0021); (2) aynı holding'deki tenant'lar arası dokunuş ve görünürlük — yalnız
`holding_*` definer'ları ve izdüşüm tetikleyicisi; üyeliği yalnız `op_*` yazar (ADR 0027)."*
ADR 0002 md.7 (KF/KM örneği), ADR 0004/0007 (#1), ADR 0016 §2 (korunur: KM oturumundan hiç
`WithTenant(KF)` açılmaz), ADR 0021 (başlık, "TEK", §3.1, §6 testlerinin `holding_*` karşılığı),
ADR 0023 (K-1c), ADR 0026 (K-1b — değişmediği notu), `docs/plan/open-questions.md` Y2.

### 4.2 API anahtarı

- `api_keys(tenant_id, id, name, token_hash, prefix_hint, created_by, created_at, last_used_at, revoked_at)` — RLS beşlisi; `token_hash` **küresel UNIQUE**.
- Kimlik: Bearer → biçim kontrolü (ortamın öneki + 43 base64url; değilse DB'ye gitmeden 401) →
  **IP başına kova** → **yedinci resolver** `resolve_api_key_by_hash` (`tappa_resolver`; ADR
  0002 §7: girdi yalnız hash, dönüş UNIQUE'le tek satır). **"Taşı, uygulama"**
  (`internal/db/resolve.go:100-102`): dönüş `(tenant_id, key_id, revoked_at, api_enabled_at)`;
  iptal → 401, kapalı tenant → 403. Önbellek yok. `tappa_resolver`'a yalnız
  `SELECT (id, api_enabled_at) ON tenants` (ADR 0002 notu).
- **Yetki (spec §4 tablosu, tenant cinsinden):** yazma = anahtarın tenant'ının çalışan ve
  lokasyonları; `scope=location` = olayın **hedef tenant'ı** anahtarınki ve rolü `location`
  (anahtarın lokasyonlarındaki bütün punch'lar, holding'deki diğer tenant'ın çalışanları dahil);
  `scope=employer` = hedef tenant anahtarınki ve rolü `employer` (anahtarın çalışanlarının
  holding'deki her yerdeki punch'ları); rehber holding geneli (`holding_directory`).
- Panel (owner): oluştur (bir kez göster) · listele · iptal. Audit `api_key.created/revoked`.
- **Oran sınırı:** IP başına kova resolver'dan önce, geçerli anahtarda anahtar başına kova.
- **Log ve tarama:** anahtar, hash'i, `Authorization` başlığı, uç nokta imza anahtarı, webhook
  URL'si ve `*url.Error` metni asla loglanmaz. Tarayıcı eklemeleri **önce bugünkü ağaçta
  sayılır** (yalın `kek`/`signature`/`authorization` kökleri 35 satırı kırmızıya çevirirdi):
  R7'ye yalnız ağacı kırmızıya çevirmeyen kökler (`api_?key`, `bearer`, `signing_?key`) ve log
  çağrısında `Header.Get("Authorization")` arayan dar kalıp; R7b'ye yeni ad alanları; R7d'ye
  değer biçimli kalıplar (iki önek). Her ekleme kasıtlı ihlalde kırmızı testiyle.
- **Env anahtarları** (`TAPPA_API_KEY_HMAC_KEY`, `TAPPA_WEBHOOK_KEK`) `namedKeys()`'e girer ve
  **bütün** anahtarlarla çift çift karşılaştırılır (`config.go:688-692` genelleştirilir; API-13
  runbook'u deploy öncesi prod çiftlerini değer basmadan ölçer).

### 4.3 Punch olayı — yapısal yayın

- **Tablolar (API-2, RLS beşlisi):** `punch_events(tenant_id, seq, punch_id, punch_tenant_id,
  roles, version, status, created_at)` — `tenant_id` = **hedef** tenant (olayı görecek olan);
  `roles ⊆ {employer, location}`; `UNIQUE(tenant_id, seq)`; append-only tetikleyicileri; R3
  kapsamı. `feed_counters(tenant_id, last_seq)`. `webhook_endpoints`, `webhook_deliveries`.
- **Hedef satırlar:** aynı tenant'ta punch → tek satır, `roles = {employer, location}`.
  Holding punch'ı → **iki satır**: işveren (KM) için `{employer}`, plaketin tenant'ı (KF) için
  `{location}`. Her hedef satır kendi tenant'ının sayacından `seq` alır.
- **Olayı veritabanı tetikleyicisi yazar** (binary'den bağımsız): `AFTER INSERT ON transactions`
  (K-4) ve `ON transaction_reviews` (`approved` ∧ bağlı satır K-4'ün kanal/practice şartını
  sağlıyorsa). Saf `tap.Decide`, `policy` ve sayaç ilerletme yolu dokunulmaz.
- **Yetki modeli:** **kendi tenant'ına** yazan kısım `tappa_feedwriter` (NOLOGIN, **NOBYPASSRLS**,
  SECURITY DEFINER) — 3.–5. turda ölçüldü ve onaylandı: yetkisi tam olarak `punch_events` INSERT,
  `webhook_deliveries` INSERT, `feed_counters` SELECT(tenant_id, last_seq)/INSERT/UPDATE(last_seq),
  `USAGE ON SCHEMA public`, okuma sütunları `transactions (id, tenant_id, channel, practice,
  verdict, location_id, employee_id, site_tenant_id)`, `employees (id, tenant_id)`,
  `webhook_endpoints (id, tenant_id, active)`, `tenants (id, api_enabled_at)`. **Plaketin
  tenant'ına** (KF) yazan kısım — KF hedef satırı, KF teslim satırı, `site_punches` — GUC KM
  iken RLS `WITH CHECK`'e takılacağı için **`tappa_holdingdefiner`** sahipli ayrı tetikleyici
  fonksiyonudur (§4.1 istisnasının parçası); yazdığı satırın `tenant_id`'si yalnız
  `NEW.site_tenant_id` olabilir ve eşlik şartı gövdede yeniden sınanır.
- `tappa_app` bu tablolarda §4.0'a tabi: `punch_events`, `feed_counters`, `site_punches` SELECT;
  `webhook_deliveries` SELECT + kira/sonuç sütunlarına UPDATE. INSERT/DELETE hiçbirinde yok.
- **Rollerin yaşam döngüsü ve katalog testi** (her iki definer rolü): `01-roles.sql` idempotent
  blok + runbook; migration ön koşulu rol yoksa ya da iki yönden herhangi bir üyeliği varsa düşer
  (00029:99-120); `internal/db/pool.go`'nun rol reddi sertleştirilmiş ortamlarda `tappa_app`
  herhangi bir rolün üyesiyse açılışı reddeder (bugün `tappa_app`'in üyeliği 0 — 5. tur ölçtü);
  katalog: NOLOGIN, beklenen BYPASSRLS değeri, üyesiz, sahip olunan tetikleyici fonksiyonlarında
  `prorettype = trigger` ve EXECUTE ACL'i **tam olarak** sahip; `proconfig` sabit. ADR 0021'in
  `prosecdef` sahip testi iki rol ile genişler.
- **Değişmezler:** `SET search_path`, nitelikli adlar, açık tenant filtreleri ve GUC assert'i
  (kendi-tenant kısmı); hata yutan `EXCEPTION` bloğu yok; uç nokta adresini okumaz; ağ/uzun iş yok.
- **Teslim satırı:** hedef tenant'ın API erişimi açıksa (K-22) ve aktif uç noktası varsa.
- **Geri doldurma (tek migration işlemi):** `transactions` **ve** `transaction_reviews` kilitlenir,
  tetikleyiciler kurulur, sonra geçmiş uygun satırlar için olay yazılır (bugün holding satırı
  yok → yalnız tek-tenant satırları); teslim satırı yazılmaz; kilit süresi prod boyutunda ölçülür,
  **eşik 2 sn**.
- **Eşlik eden listeler:** `cmd/tappa/scriptguards_test.go:447-510` (`APPEND_ONLY` —
  `punch_events` ve `site_punches`), `scripts/pg-restore-verify.sh` (`trunc_tables`, sabit
  "seven" cümlesi).

### 4.4 Akış imleci — neden kaybolmaz

Tetikleyici her hedef satırdan önce hedef tenant'ın sayacını
`INSERT … ON CONFLICT (tenant_id) DO UPDATE SET last_seq = last_seq + 1 RETURNING last_seq` ile
artırır; satır kilidi commit'e kadar. Aynı hedef tenant'ta iki olay-yazan işlem sayacı sırayla
artırır ve o sırayla commit eder → `WHERE tenant_id = $t AND seq > $cursor ORDER BY seq` hiçbir
satırı atlamaz; abort sayacı da geri alır. **3. turda ölçüldü** (tek hedef): 3 koşuda 0 kaçan,
iki mutasyon (kilitsiz dizi, ayrı işlemde sayaç) 843/408 kaçırdı; ilk-olay yarışı 1700 olayda 0.

- **İki hedef (holding punch'ı):** bir işlem iki sayaç satırı kilitler. **Kanonik sıra:** her
  zaman küçük tenant id'li sayaç önce. Aksi hâlde KM@KF (KM sonra KF) ile KF@KM (KF sonra KM)
  ters sırada kilitleyip kilitlenirdi. API-2'nin stres testi iki-hedefli ve ters yönlü
  dokunuşları karışık koşturur: **0 kilitlenme, 0 kaçan**.
- **Kilit değişmezi:** sayaç satırları kilitlendikten sonra çakışabilecek kilit alınmaz, ağ/uzun
  iş yapılmaz.
- **Takılı yazıcı:** `tappa_app` bağlantıları için `idle_in_transaction_session_timeout` ve TCP
  keepalive **havuzun `RuntimeParams`'ıyla**, açılışta `SHOW` ile doğrulanır; tetikleyici
  fonksiyonları **öznitelik** olarak kısa bir `lock_timeout` taşır (fonksiyon çıkışında geri
  yüklenir; ~505 ms'de 55P03 ölçüldü). Bekleyeni `lock_timeout`, takılı yazıcıyı idle zaman aşımı bitirir.
- **Sayaç satırı her tenant için her zaman vardır:** migration mevcut tenant'lara açar, yeni
  tenant'ınkini `tappa_feedwriter` sahipli `AFTER INSERT ON tenants` tetikleyicisi açar.
- **Geri yükleme:** uygulama yazmaya başlamadan önce runbook her tenant'ın sayacını büyük bir
  aralık ileri atar; `scripts/pg-restore-verify.sh` her tenant için `last_seq ≥ max(seq) +
  aralık`'ı geri yüklenmiş verinin kendisinden denetler, sağlanmıyorsa reddeder. Gece geri
  yükleme provası yok — T45 tipi elle provada sınanır.
- Geçmiş sorgusu (`from`/`to`): `occurred_at`, `punch_events` üzerinden; ≤ 31 gün, sayfalı.

### 4.5 Webhook

- `webhook_endpoints(tenant_id, id, url, signing_key_sealed, active, created_at, rotated_at)` —
  **`UNIQUE(tenant_id)`** (tenant = sistem başına tek uç nokta); imza anahtarı
  `sun.Seal(TAPPA_WEBHOOK_KEK, AAD = endpoint id)` ile mühürlü, **önekli** üretilir (R7d);
  rehberdeki bilinen-cevap vektörünün anahtarı açıkça sahte.
- **URL kuralı:** `https`, port 443, IP literali yok, userinfo/sorgu/parça yok; URL ve `*url.Error`
  metni asla log'a/audit'e yazılmaz (yalnız uç nokta kimliği + hata sınıfı).
- `webhook_deliveries(tenant_id, id, endpoint_id, punch_event_seq, attempt, next_attempt_at,
  lease_until, state, last_status, last_error_class, created_at, delivered_at)` —
  `UNIQUE(endpoint_id, punch_event_seq)`; `last_error_class` kapalı küme; uzak yanıt metni saklanmaz.
- **Yönlendirme (spec §4.5):** her hedef olay satırı kendi tenant'ının uç noktasına bir teslim
  doğurur → KF@KF: 1 (KF) · KM@KM: 1 (KM) · KM@KF: 2 (KM `employer`, KF `location`) · KF@KM: 2.
- **Yük izin listesi:** spec §5 + `channel`; IP, GPS, mesafe, not, policy bağlamı asla. Test ping
  sentetik; ping ve "yeniden gönder" aynı kovada (tenant başına saatte 10).
- **İşçi — kiralama:** kısa işlemde `SKIP LOCKED` + `lease_until` + commit → HTTP işlem dışında →
  sonuç ayrı işlemde. Gönderimde ve "yeniden gönder"de K-22 **ve** uç noktanın `active`'i yeniden
  kontrol edilir. Geri çekilme 1 dk · 5 dk · 30 dk · 2 sa · 6 sa → `dead`.
- **İşçi kurulumu (K-19):** süreç içi "işi olan tenant'lar" kümesi; tenant o tenant'ta bir
  dokunuş/onay commit edildiğinde (holding punch'ında **iki** tenant da), kimliği doğrulanmış her
  API isteğinde ve "yeniden gönder"de girer; okuma her zaman `WithTenant` içinde.
- **SSRF:** kontrol `net.Dialer.ControlContext` içinde gerçekten bağlanılan adrese; `Unmap()`;
  iki aile için IANA özel amaçlı kayıtları birebir; IPv4 küresel unicast; IPv6 **yalnız
  `2000::/3`**, `2001::/23` tamamı, `3fff::/20`, `2002::/16` düşülerek; düğümün genel adresleri ve
  ingress adresi değil (liste boşsa prod ve sandbox başlamaz); `Proxy: nil`; yönlendirme yok;
  10 sn; `MaxResponseHeaderBytes`; gövde okunmaz. Tehdit modeli ADR 0028'de (K-22 her yolda).
- **Kapanış bütçesi:** işçi `srv.Shutdown` ile eşzamanlı, 20 sn içinde; test API-10'da.

### 4.6 Ortak sözleşme

- JSON hata gövdesi `{code, message, details}`; 400/401/403/404/409/422/429/503; `Retry-After`.
  `limit` ≤ 1000 (varsayılan 500); gövde ≤ 64 KiB.
- **Idempotency-Key:** `api_idempotency(tenant_id, key_id, idem_key, request_hash, status,
  result_ref, created_at)`, `UNIQUE(tenant_id, key_id, idem_key)`; satır iş işlemiyle aynı işlemde;
  gövde saklanmaz; farklı gövde 422; 24 sa sonra istek yolunda tembel silinir.
- `GET /employees` yanıtı e-posta içermez.
- OpenAPI 3.1 elle yazılır, embed edilir, `GET /v1/openapi.yaml`; imza vektörü rehberde.

### 4.7 Askıya alma (ADR 0025 + K-26)

| Askıda açık | Askıda 403 `tenant_suspended` |
|---|---|
| bütün `GET`'ler, `/v1/punches`, `/v1/directory`, webhook gönderimi · `PUT /employees` (yeni çalışan) · `resend-activation` · davet kuyruğu işçisi · tetikleyicinin olay/teslim/izdüşüm yazması (askı dokunuşu durdurmaz — holding dokunuşu dahil) · **K-26:** anahtar iptali, uç nokta devre dışı bırakma | `PUT /locations` · var olan çalışanı değiştiren `PUT /employees` (`active:false` dahil) · `DELETE /employees` · anahtar oluşturma · uç nokta oluşturma/döndürme/adres değiştirme · test ping · "yeniden gönder" |

Tabloda adı geçmeyen her M11 yazması askıda kapalıdır. Askı sütunları OP-15'e bağlı → API-4
yalnız kontrol noktasını kurar; testleri API-4b (OP-15'e bağlı).

### 4.8 Sandbox (K-3) ve çerez geçişi (COOKIE-1)

- **COOKIE-1 (sandbox'tan önce):** ana sitenin oturum çerezleri (`tappa_session`, panel ve
  aktivasyon çerezleri) **`__Host-` önekine** taşınır: `Secure`, `Path=/`, `Domain` yok → hiçbir
  alt alan onları ezemez (cookie tossing). Geçiş **şeffaf**: sunucu bir süre eski adı da okur,
  eski çerezle gelen istekte yenisini yazıp eskisini siler; hedef kimsenin oturumdan düşmemesi
  (yapıcı ölçer; panel çerezinin `Path=/admin` olması gibi kısıtlar ADR'de çözülür). Operatör
  çerezi zaten `__Host-` (`internal/operatorauth/cookie.go:12-57`). Bitince
  `internal/handler/cookies.go:138-159`'daki kısıt "kapandı" notu alır.
- **Alt alan:** `sandbox.taptime.mt` (DNS-only), kendi ops host'u.
- **Ayrı derleme:** simülasyon kodu ayrı pakette, yalnız `sandbox` tag'li `cmd` dosyası import
  eder; prod için `go list -deps` negatif, sandbox için pozitif; CI `-tags sandbox` vet/test;
  `verify-image.sh` (`:118`) tag'i doğrular. Sandbox ikilisi yalnız `TAPPA_ENV=sandbox` ile
  başlar; tag'siz ikili `sandbox`'ı reddeder. Dev simülatörünün kapısı gevşetilmez.
- **Sertleştirme:** K-28. **Sanal plaket izin listesi:** yalnız NXP olmayan ayrılmış UID aralığı
  (gerçek NXP `0x04` ile başlar), her çözümlemede ve eklemede. Kendi sırları; prod yedeği oraya geri yüklenmez.
- **Simülasyon:** `POST /v1/sandbox/taps {employeeRef, locationRef, channel}` — holding
  dokunuşunu da simüle eder (sandbox'ta iki tenant + holding operatörce kurulur).
- **Hesap açılışı:** `/signup` prod'daki gibi; API/webhook ve holding yalnız sandbox operatörüyle.
- **E-posta:** gerçek e-posta yok. **Altyapı:** ayrı namespace + Postgres, NetworkPolicy, PSS
  `restricted`, `ResourceQuota`/`LimitRange`.

### 4.9 Pasifleştirme bütçesi (K-25)

`DELETE /employees` ve `PUT {active:false}` anahtar başına saatlik bütçeyi paylaşır (varsayılan
20). Bütçe son bir saatin audit satırlarından, anahtar başına advisory kilit altında sayılır
(`db/queries/invites.sql:403`); **kilit, sayım, pasifleştirme ve `RecordTx` audit tek işlemde**
(`internal/domain/tenant/staff.go:72-91`). Aşılınca 429, owner'a panel uyarısı, audit. ADR 0010 sapma notu.

---

## 5. Görevler

| ID | Görev | Boyut | Ajan | Bağımlılık |
|---|---|---|---|---|
| API-0 | **ADR 0027** (holding: tablolar, `op_*`, `tappa_holdingdefiner`, `holding_*` fonksiyonları, `site_*` sütunları, izdüşüm, K-1a…d, K-29, K-30) + **ADR 0028** (dış API: anahtar, punch yayını, iki-hedefli sayaç imleci, tetikleyici değişmezleri, webhook, SSRF tehdit modeli, K-19, sandbox kapısı, pasifleştirme bütçesi) + **CLAUDE.md §4.5** ikinci istisna metni, §3 (yeni paketler), §7 (log yasakları), §9 (K-1c) + notlar: ADR 0002, 0004, 0007, 0010, 0016, 0021, 0022 §7, 0023, 0025, 0026; `open-questions.md` Y2 | L | yapıcı + üçüncü göz + güvenlik | — |
| COOKIE-1 | `__Host-` çerez geçişi (şeffaf, oturum düşmeden) | M | yapıcı + güvenlik | API-0 |
| API-1 | Şema (tek-tenant): `external_ref`, `employee_code`, `first_name`/`last_name`, `employees.updated_at` + K-21 tetikleyicisi, `invite_requests`, `locations.external_ref`/`active`, K-14 CHECK, K-24 indeksi (ad korunur), `tenants.api_enabled_at` + açan/kapatan `op_*`; §4.0 katalog testi iskeleti. Panel: "External ref" alanları, manuel kayıtta lokasyon seçimi | M | `tappa-db-migrator` + yapıcı (`tappa-brand`) | API-0 |
| API-1a | Holding varlığı: `holdings`, `holding_members`, üç `op_*` (geçiş penceresi, saat dilimi eşitliği), `tappa_holdingdefiner` (`01-roles.sql` + runbook), katalog testleri, operatör ekranı | M | `tappa-db-migrator` + yapıcı | API-0 |
| API-1b | Çapraz dokunuş: `transactions.site_*` + FK + CHECK + üyelik tetikleyicisi · `holding_tap_site`, `holding_advance_tag_counter` · guardrail #1 `SameHolding` · `checkin`/`tap` değişiklikleri · tap ve sonuç ekranında KF markası (K-1c) | L | yapıcı (`tappa-sun`, `tappa-brand`) | API-1a |
| API-1c | KF izdüşümü `site_punches` + KF panel görünümleri (holding dokunuşları, işgücü, plaket son görülme) · KM panelinde KF mekan adları (`holding_site_names`) · lokasyon silme sayımı | L | yapıcı (`tappa-brand`) | API-1b |
| API-2 | `punch_events` (hedef tenant + roller), `feed_counters` (+ `tenants` tetikleyicisi), `webhook_endpoints`, `webhook_deliveries` · `tappa_feedwriter` + holding tarafı tetikleyici · kanonik kilit sırası · geri doldurma · zaman aşımları · geri yükleme atlatma + doğrulayıcı · eşlik eden listeler | L | `tappa-db-migrator` | API-1, API-1b |
| API-3 | `api_keys` + resolver + `internal/apikey` + panel + audit + R7/R7b/R7d + env anahtar ayrımı | M | yapıcı (`tappa-brand`) | API-1 |
| API-4 | `/v1` iskeleti: host kapısı, biçim + IP kovası + Bearer + anahtar kovası, JSON hata, gövde sınırı, Idempotency-Key, askı kontrol noktası, `openapi.yaml` iskeleti, K-19 işçi iskeleti | M | yapıcı | API-3 |
| API-4b | Askı tablosunun testleri | S | yapıcı | API-4, **OP-15** |
| API-5 | `PUT /v1/locations/{ref}` | S | yapıcı | API-4 |
| API-6 | `PUT/GET/DELETE /v1/employees/{ref}`, `resend-activation`, davet kuyruğu işçisi, K-25 | L | yapıcı | API-4 |
| API-7 | `GET /v1/punches` (iki kapsam, imleç, `from/to`) | M | yapıcı | API-2, API-4 |
| API-8 | `GET /v1/directory` (`holding_directory`) | S | yapıcı | API-1a, API-4 |
| API-9 | Uç nokta paneli + SSRF-güvenli istemci | M | yapıcı (`tappa-brand`) | API-2, API-4 |
| API-10 | Webhook gönderici + teslim günlüğü | L | yapıcı (`tappa-brand`) | API-9 |
| API-11 | Sandbox ikilisi + simülasyon + kurulum | M | yapıcı + kullanıcı (DNS) | **COOKIE-1**, API-5…API-10 |
| API-12 | OpenAPI + entegrasyon rehberi (EN; §2'nin 26 maddesi + imza vektörü) | S | yapıcı | API-5…API-11 |
| API-13 | Canlıya alma: sırlar, `api.` DNS + Ingress, **`tappa-security-auditor` tam tur**, operatörden K-22 açılışı ve **KF+KM holding'inin kurulması**, KF/KM bağlama runbook'u (K-18), prod anahtar çifti ve `pg_auth_members` ölçümü, spec §7 kabul listesi | M | yapıcı + denetçi + kullanıcı | hepsi + **OP-15** |

Her görev: yapıcı (opus) → **ayrı** üçüncü göz → bulgu varsa düzelt + yeniden denetle;
CLAUDE.md §4'e değen görevlerde (API-0, API-1a, API-1b, API-1c, API-2, API-3, COOKIE-1)
ayrıca `tappa-security-auditor`. Büyük testler yalnız görev sonunda bir kez. Dal: `m11-api`
(`main`'e birleştirme = deploy kararı, kullanıcının). Her commit'ten önce
`./scripts/redline-check.sh` (pre-push kancası her commit'i tarar).

### Kabul çekirdeği (kart açılırken genişler — burada bağlayıcı)

- **API-0:** CLAUDE.md §4.5 yeni metni (iki istisna, adlarıyla); her ADR notu; ADR 0027'de holding tehdit modeli (holding dışı tenant'ın hiçbir yoldan erişememesi), ADR 0028'de SSRF tehdit modeli ve sandbox kapısı normatif.
- **COOKIE-1:** her oturum çerezi `__Host-` önekli, `Secure`, `Path=/`, `Domain`'siz; eski çerezle gelen oturum düşmeden yenisine geçer (çalışan, panel, aktivasyon için ayrı ayrı test); bir alt alanın `Domain=taptime.mt` ile yazdığı çerez ana sitenin oturumunu ezemez (test); `cookies.go`'daki kısıt notu güncellenir.
- **API-1:** §4.0 katalog testi (CI'da `arwd` varsayılanı) yeni tabloları kapsar; K-24 indeksi ve kısıt adı (panelde 409, 500 değil); pasif satırla aynı e-postada yeni aktif satır açılır, iki aktif satır açılamaz; `updated_at` dört UPDATE yolunda ve yalnız rehbere görünen sütun değişince; `location_id` okuyucu sayımı raporda; `api_enabled_at`'ı `tappa_app` yazamaz.
- **API-1a:** `holdings`/`holding_members`'a `tappa_app` hiçbir fiille dokunamaz (`arwd` altında); yalnız `op_*` yazar ve audit hem operatör hem iki tenant günlüğünde; farklı saat dilimli tenant eklenemez; bir tenant iki holding'e üye olamaz; `left_at` geri alınamaz; `tappa_holdingdefiner` NOLOGIN, üyesiz, fonksiyon ACL'leri tam olarak beklenen; geri yükleme sıfırdan kurulmuş pod'a.
- **API-1b:** (a) KM oturumu + KF plaketi → kayıt **KM'de**, `site_*` dolu, `location_id`/`tag_uid` boş; (b) **holding dışı tenant C**: C oturumu + KF plaketi → bugünkü gibi 403, kayıt yok, KF sayacı ilerlemez; `holding_*` fonksiyonlarının hepsi C bağlamında 0 satır; (c) aynı `(tag, ctr)` ile N goroutine, yarısı tek-tenant yarısı holding yolu → **tam 1** başarı (`-race`); (d) KF plaketine dokunuş ardından 60 sn içinde KM plaketine dokunuş → `ignored`; KF'de giriş + KM'de çıkış → doğru `out`; (e) karar KM politikasıyla (KF'nin `location/<id>` istisnası uygulanmaz — test adı bunu söyler); (f) geçiş penceresi: üyelik çıkarıldıktan sonra 5 dk içinde karara girmiş dokunuş yazılır, sonra 403; (g) KM davetlisi KF plaketinde **aktive olamaz** (K-1b); (h) tap ve sonuç ekranı KF logosu/accent'i (VIES kapısı dahil); (i) CHECK `site_tenant_id = tenant_id`'yi ve yarım dolu `site_*`'ı reddeder; üyelik tetikleyicisi eş olmayan `site_tenant_id`'yi reddeder; (j) `holding_advance_tag_counter` replay korumasını korur (sayaç geriye gitmez, tekrar 0 satır).
- **API-1c:** KF izdüşümünde GPS/IP/not/policy bağlamı sütunu **yok** (katalog); KF, KM çalışanının KF sitesindeki girişini ve onu kapatan KM çıkışını görür; KF onay veremez; KM raporunda vardiya "KF — <mekan>" kovasında; KF lokasyon silme holding referanslarını sayar; C tenant'ı izdüşümden 0 satır görür.
- **API-2:** (a) düz `INSERT INTO transactions` olay doğurur; (b) çok oturumlu stres (≥ 12 yazıcı, ≥ 3 tenant, holding'de **iki-hedefli ve ters yönlü** dokunuşlar karışık, abort/savepoint karışık, eşzamanlı okuyucu) → **0 kaçan, 0 kilitlenme**, `seq` yoğun; mutasyonlarda kırmızı: kilitsiz dizi, ayrı işlemde sayaç, **ters kilit sırası** (kilitlenme); (c) geri yükleme doğrulayıcı atlatmasız reddeder, atlatmayla yeni olay eski imlecin ötesinde; (d) flag olay yazmaz, onaylanınca yazar; manuel/practice asla; (e) C tenant'ı A'nın olaylarını görmez (filtresiz); (f) `arwd` altında `tappa_app` ile `punch_events`/`feed_counters`/`site_punches` INSERT/UPDATE/DELETE 42501; `webhook_deliveries` INSERT/DELETE ve kira dışı UPDATE 42501; geçici tabloya tetikleyici bağlama 42501 (her iki definer'ın fonksiyonları için); (g) tetikleyici hatası kaydı geri alır; (h) savepoint geri alımı sayacı geri alır; (i) `lock_timeout` bekleyeni bırakır, idle zaman aşımı takılı yazıcıyı bitirir, öznitelik sızmaz; (j) holding punch'ı iki hedef satır: KM `{employer}`, KF `{location}`; tek-tenant punch tek satır `{employer, location}`; (k) kapalı API erişiminde olay yazılır, teslim yazılmaz; (l) geri doldurma ≤ 2 sn; (m) iki definer rolünün katalog testi; (n) sıfırdan kurulmuş pod'a geri yükleme.
- **API-3:** katalog testi `api_keys`'i kapsar, iptal geri alınamaz, eşzamanlı çift ve toplu iptal anahtarı canlı bırakmaz; iptal → 401, kapalı tenant → 403; yanlış önek DB'ye gitmeden 401; anahtar log/hata/audit'te yok; yeni tarayıcı kökleri kasıtlı ihlalde kırmızı; aynı değerli iki env anahtarıyla süreç başlamaz; C'nin anahtarı A'nın satırını okuyamaz/yazamaz.
- **API-4:** katalog testi `api_idempotency`'yi kapsar; host kapıları; IP kovası resolver'dan önce; Idempotency eşzamanlılığı ve takılmazlık.
- **API-4b:** §4.7 tablosunun her hücresi.
- **API-5:** C'nin anahtarı KF lokasyonunu göremez; farklı `timezone` 422; tekrar `PUT` yeni satır açmaz.
- **API-6:** e-posta değişince eski davet aynı işlemde ölü; resend döngüsü canlı daveti öldürmez (N çağrı → en çok 1 gönderim); aktive çalışana resend 409; kuyrukta kod yok; iki pod aynı daveti bir kez basar; pasifleştirme bütçesi DB'den, yeniden başlatmada sıfırlanmaz, 1 hak kalmışken N eşzamanlı → tam 1 başarı; `GET`'te e-posta yok.
- **API-7:** dört şekil: KF@KF → KF anahtarının iki kapsamında; KM@KM → KM'nin iki kapsamında; **KM@KF → KF `location` + KM `employer`**; **KF@KM → KM `location` + KF `employer`**; C'nin anahtarı hiçbirini görmez; manuel/reddedilmiş yok; `limit` 1001 → 400.
- **API-8:** rehber holding'deki iki tenant'ı döner, C'yi dönmez, e-posta yok; örtüşme penceresi kaçırmaz.
- **API-9:** katalog `webhook_endpoints`'i kapsar; tenant başına ikinci uç nokta açılamaz; anahtar ikinci kez gösterilemez; ping/yeniden gönderim aynı kova; kapalı erişimde reddedilir.
- **API-10:** dört yönlendirme şekli (KF@KF 1, KM@KM 1, KM@KF 2, KF@KM 2); SSRF reddi listesi (`169.254.169.254`, `0.0.0.0`, `::ffff:127.0.0.1`, `::7f00:1`, `64:ff9b::`, `2001::1`, `2001:1::1`, `2001:2::1`, `3fff::1`, düğüm adresi, `localhost`'a çözülen ad, `http://`, userinfo/sorgulu URL, 302); iki aşamalı DNS reddi; devre dışı uç noktaya teslim yok; `HTTP_PROXY`'ye rağmen doğrudan; iki pod tek gönderim; gönderimde açık DB işlemi yok; kapanış bütçesi.
- **API-11:** COOKIE-1 bitmiş; prod derlemesinde simülasyon yok; CI `-tags sandbox`; deploy kapısı tag'i doğrular; gerçek UID reddi; dokuz `IsHardened` kapısı sandbox'ta; operatörsüz anahtar 403; simülasyon holding dokunuşunu da üretir.
- **API-12:** OpenAPI her uç ve hata kodu; §2'nin 26 maddesi.
- **API-13:** spec §7'nin dokuz maddesi (6. madde sözleşme farkı); güvenlik ONAY; KF+KM holding'i operatörce kuruldu, holding dışı tenant'ların izolasyonu prod'da örneklendi.

---

## 6. Riskler ve tuzaklar

- **CLAUDE.md §4.5 ikinci istisna** — holding dışı izolasyon her `holding_*` fonksiyonu ve izdüşüm için ayrı testle kanıtlanır; `holding_members`'a `tappa_app`'in yazabildiği tek yol, herhangi bir tenant'ın KF'nin holding'ine katılması demek olur (§4.0 REVOKE + katalog).
- **CLAUDE.md §4.4** — holding ilerletmesinin replay ifadesi ikinci bir kopya; kayma riskini çekişme testi tutar.
- **Kanonik kilit sırası** — iki-hedefli punch'ta ihlal kilitlenme üretir; stres testi ters yönlü dokunuşlarla.
- **Üyelik geçiş penceresi** — 5 dk'dan uzun bir karar→kayıt aralığı (olağan değil) reddedilen kayda dönüşür; değer ADR'de.
- **KF'nin şube politikası** KM çalışanına uygulanmaz (K-1a bedeli).
- **R7 log tarayıcısı** ve R7d doküman taraması (bu dosyanın 1. sürümü push'u durdurdu).
- **Tek replika, bellekte limitler, K-19 kümesi** — yeniden başlamada boşalır (§2 madde 20).
- **Davet tavanları ve süreç kesicisi** ilk toplu senkronu saatlere yayar — runbook'ta.
- **Pilot verisinin çiftlenmesi** → K-18 runbook'u ilk `PUT`'tan önce.
- **GDPR** — K-1d görünürlüğü DPA/aydınlatma metnine yazılır (kullanıcı).
- **Egress açık** — SSRF kapısı tek savunma.
- **Canlı ön koşul:** OP-15 (API-4b). EM-12 artık ön koşul değil.

## 7. Kullanıcının dış adımları

1. ~~K-1 (holding), K-1a…d, K-2, K-3 (alt alan), K-19, K-25, K-26~~ ✅
2. `api.taptime.mt` ve `sandbox.taptime.mt` DNS kayıtları (Cloudflare, DNS-only).
3. KF-RMS / KM-ERP canlı + test webhook adresleri — API-13'te.
4. Canlı öncesi: KF ve KM panellerindeki lokasyon/çalışanların bağlanması (K-18).
5. DPA / aydınlatma metnine holding içi paylaşım notu (K-1d).
6. Canlı ön koşul: OP-15.

## 8. Kapsam dışı (v1)

Punch yazma API'si (spec §9) · punch iptali/düzeltmesi · departman uçları · lokasyon silme API'si ·
webhook yönetimi API'den · lokasyon başına saat dilimi · KF'nin KM çalışanı için onay vermesi ·
holding'e ikiden fazla tenant için ek ürün yüzeyi (model destekler, UI tek holding'i varsayar) ·
OAuth · çoklu replika için paylaşımlı oran deposu · egress NetworkPolicy.

---

## 9. Denetim izi

**1.–5. tur (2026-10-09/10; `a169391` … `690c69b`) — "tek tenant, iki şirket" modeli.**
Ayrıntılı bulgu tabloları `690c69b`'deki sürümde (git geçmişi). Özet: 1.–3. tur iki mercekte RED;
4. tur üçüncü göz ONAY; 5. tur güvenlik ONAY. Ölçümle çürütülüp yerine konanlar ve **bu sürümde
aynen korunanlar:** olay Go katmanında → DB tetikleyicisi · `xid8` imleci → sayaç satırı · küresel
dizi (`CACHE 20`'de 1016/1234 kaçtı) → tenant sayaç satırı (0 kaçan) · yalnız INSERT REVOKE (prod
`arwd`) → §4.0 · tetikleyici fonksiyonunun PUBLIC EXECUTE'u (geçici tablo yolu) → `REVOKE … FROM
PUBLIC` · `tappa_feedwriter`'ın eksik yetki seti → ölçülmüş tam liste · SSRF yasak listesi → izin
listesi · askı ADR 0025'le hizalı · `IsHardened` · Idempotency aynı işlemde · pasifleştirme bütçesi
DB'de ve tek işlemde. **Geçersiz kalan:** şirket boyutuna bağlı her şey (K-24'ün şirket hâli,
K-27, şirket başına tavan, ikinci şirket `op_*`'ı, KM plaket taşıma, EM-12 ön koşulu).

**Model değişikliği (2026-10-10, kullanıcı).** 5. sürüm onaylandıktan sonra kullanıcı: *"taşıma
yapmayacağız; o aynı holding altında ayrı iki firma, ayrı iki tenant"* → K-1 holding; ardından
K-1a (kayıt işverende), K-1b (aktivasyon kendi plaketinde), K-1c (KF markası), K-1d (görünürlük
DPA notuyla); K-3 alt alan + `__Host-` çerez geçişi. Tasarım, kod üzerinde çıkarılmış bir etki
haritasına dayanır (tap yolu P1–P12, `transactions` FK'leri, RLS seçenekleri (i)/(ii)/(iii) —
(i) RLS dalı satırın tamamını açtığı ve tarayıcıların göremediği için elendi).

**6. tur:** bekliyor — holding parçaları tam, değişmeyen parçalar gerileme taraması.
