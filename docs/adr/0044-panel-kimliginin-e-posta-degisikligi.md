# ADR 0044 — Panel kimliğinin e-posta adresini değiştirmek: yeni adresin posta kutusu kanıtı, eski adrese bildirim ve geri alma, aynanın definer'la güncellenmesi

- **Durum:** **Önerildi (2026-11-11)** — kart yazıldı, iki bağımsız inceleyicinin onayı ve kullanıcının §10'daki
  kararları bekleniyor. Kod, migration ve CLAUDE.md değişikliği **yok**. Kullanıcı kararı (2026-11-11):
  *"parti 7'de kartını yaz"*; önce kart, iki bağımsız onay, **sonra** kod.
- **Tarih:** 2026-11-11 · **Backlog:** T173 · **Dal:** `m14-emailcard` (taban `origin/m14-identity` 2624a89a)
- **Ayrılmış numaralar:** migration **00101–00103** (kodu yazacak kartlar için; bu dalda dosya yok) · artık
  satırlar için backlog T294–T297.
- **Kart:** [docs/plan/t173-eposta-degistirme.md](../plan/t173-eposta-degistirme.md) (EC-1, EC-2, EC-3).
- **İlgili:** [ADR 0031](0031-tek-kimlik-cok-isletme.md) §5.1, §5.2 (kapalı liste + fiil × sütun matrisi),
  §5.6 (düz zamanlama), §5.8 (onay penceresi), §5.9, §5.14, §5.15, §6.1, §8, §11, §13 K-14/K-15 ·
  [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) · [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) ·
  [ADR 0022](0022-islemsel-eposta.md) §6, §9, §10 ve EM-9 notu · [ADR 0035](0035-panel-oturumu-sunucuda-biter.md) ·
  CLAUDE.md §4.5 (3), §4.6, §6, §7 · open-questions Q34.
- **Kelimeler:** **okundu** = dosyadan, çalıştırılmadı. Bu ADR'de **ölçülen yoktur**; tek istisna `grep`
  sayımlarıdır ve öyle yazılır. Atıflar (`yol:satır`) `m14-emailcard` tabanı 2624a89a'ya göredir.

---

## 1. Sorun

M14'ten beri bir panel yöneticisinin e-postası **kişiye** aittir: `admin_identities.email` global tekildir
(K-1) ve `admin_users.email` her üyelikte onun **aynasıdır** (K-14). Adresi değiştirmenin bugün **hiçbir
yolu yoktur** ve bu bilinçli bir karardır (K-15 = (a), kullanıcı 2026-10-22). Var olan bir kimliğin adresi
yanlış yazılmışsa, işten ayrılan biri iş adresini bırakıyorsa ya da işletme alan adını değiştiriyorsa,
kişinin tek çaresi bugün yeni bir kimlik açmak ve üyelikleri davetle yeniden kurmaktır. Bir kez canlıda elle
yapıldı: KM'nin panel adresi kullanıcı onayıyla, orkestratörün tek seferlik yazımıyla KF'ninkine çevrildi ve
KM audit'ine adressiz bir `admin.email.changed` satırı yazıldı (`docs/plan/state.md:807`, 2026-10-30 notu;
okundu). Bu ADR o işin ürün yolunu tasarlar.

## 2. Bugün — kanıtla (okundu)

| Olgu | Kanıt |
|---|---|
| E-posta değiştirme M14'ün dışında; kendi kartı ve kararıyla gelir; beş gerek sayılı: mevcut parola, yeni adresin doğrulanması, eski adrese bildirim, çakışmada aynı "gelen kutunu kontrol et", aynanın bütün işletmelerde definer ile güncellenmesi | ADR 0031:1573 (K-15), :1545 (§11) |
| `tappa_app`'ın `admin_users` üzerindeki `UPDATE (email)`'i expand'de kapandı; ayna uygulamaya yazılamaz | `db/migrations/00069_add_admin_identities.sql:251`; ADR 0031:196-197, :1572 (K-14) |
| Bağlı satırın `email`'ini `tappa_identitydefiner` dışında kim değiştirirse değiştirsin tetikleyici reddeder (INVOKER, `current_user` okunur) | `00069:264-290` (birinci kol `:279-285`) |
| Definer'ın matrisinde **`UPDATE (email)` yok** — ne `admin_identities`'te ne `admin_users`'ta; "gelirse kendi genişlemesini getirir" | ADR 0031:742; `00072:254-270` (*"never email (K-15)"*) |
| `admin_identity_verifications` yalnız var olan adresi doğrular; yeni adres için sütun yok | `00072:188-210` (yorum `:190-192`); ADR 0031:153 |
| Doğrulama tablosu: `token_hash` 64 hex (token saklanmaz), `UNIQUE (token_hash)`, 24 saat tavanı CHECK'te; `purpose` `'verify'` \| `'link'` (00084) | `00072:193-206`; `00084:81-84` |
| Link token'ının hash'i amaca özgü türetilmiş bir HMAC anahtarıyla; iki amacın token'ı birbirinin hash'ine çözülmez | `internal/adminauth/identityaccount.go:58-68`; ADR 0031:574 |
| Doğrulama linki GET'te harcanmaz: değer kısa ömürlü çereze girer (30 dk), harcama POST'ta ve tıklayan tarayıcıda aynı kimliğin canlı oturumu ister (R2, R19) | `internal/handler/accountidentity.go:68`, `:263-276`; `identityaccount.go:221-248`; ADR 0031:324 |
| Hesap sayfasının kimlik eylemleri kişiyi **oturumdan** bulur; satırın adresi kimliğe çözülür ve oturumun kişi kovasına karşı denetlenir | `identityaccount.go:130-153` (`identityOfSession`) |
| Parola değişimi: Go mevcut parolayı `identity_password_for_session`'ın digest'iyle karşılaştırır; `identity_set_password` kimliğe ve bağlı her satıra yazar, **öbür** oturumları her işletmede kapatır, her işletmeye kendi `admin.password_changed` satırını yazar | `internal/adminauth/changepassword.go:106-180`; `00085:417-494` (audit `:480-482`) |
| Bağlı satırın ve kimliğin adresi her an eşit olmalı; aksi hâlde `identityOfSession` `ErrNotLinked` verir ve hesap eylemleri kırılır | `identityaccount.go:139-151` |
| Kimlik başına sıfırlama linki **kimliğin kayıtlı adresine** gider; id ile alıcı aynı çözümleyici satırından, Go tipiyle bağlı | `internal/adminauth/identityreset.go:31-44`; ADR 0031:462-473 |
| Kimlik düzeyi bir linkin talebi/teslimi **audit satırı değil** bütçeli bir log satırıdır; işletmelerin izi yalnız tenant'a yazan tüketimdir | ADR 0031:483-491 |
| Anonim sıfırlama talebinin yanıtı her adreste aynı; gönderim istek yolundan çıkar (outbox), yanıt bir tabana (250 ms) bekler; kimlik yolunun düzlüğü HTTP'de ölçülür | ADR 0022 §6 (`:369-400`); `internal/handler/adminresetlimits.go:347`; `internal/handler/identityreset_db_test.go:110-158` |
| Girişin düzlüğü "eşit iş"ten gelir: beş başarısızlık durumu aynı çözümleyicileri, aynı karşılaştırma sayısını ve aynı olay çağrısını öder | ADR 0031:916-935, :937-950 (M14-07 notu) |
| "Parolanız değişti" bildirimi istek içinde, outbox'tan **değil**, kesiciden `SendExempt` ile; hesap başına saatte 5; kimlik yolunda bildirim satırı hiçbir işletmeye yazılmaz | `internal/handler/passwordnotice.go:27-45`, `:221-222`; `identityreset_db_test.go:105-107` |
| Kişi başına oranlar: doğrulama linki saatte 5, parola kapılı hesap eylemleri 10 dk'da 10 | `internal/handler/adminratelimit.go:851-860` |
| E-posta log'a yazılmaz; gönderim log satırının anahtarları kapalı küme | CLAUDE.md §7; ADR 0022 §10 (`:617-638`); ADR 0031 §5.15 (`:1297-1302`) |
| Kimlik olayları kapalı küme (`confirm_kept` dahil sekiz), `detail` yalnız `tenant` ve `scope` | `00072:228-238`; `00079:113-117` |
| Adresin danışma kilidi `hashtextextended('admin-identity-email:' \|\| lower(email), 0)`; kayıt sahiplenmesinin parola kolu önce adres kilidini, sonra kimliği alır | `00074:176`; ADR 0031:383-386 |
| Tenant içinde e-posta tekil (kısmi UNIQUE) — aynanın güncellemesi aynı işletmedeki başka bir satırla çakışabilir | `db/migrations/00006_create_admin_users.sql:96-97` |
| İhlal hatasının `DETAIL`'i satırın değerlerini (adres dahil) hataya ve sunucu log'una basar | ADR 0031:261-265 |
| Migration Job rollout'tan önce koşar; tek replika, `maxSurge: 1`, `maxUnavailable: 0` — eski pod yeni şemada çalışır | `deploy/k8s/20-app.yaml:53`, `:65-66`; ADR 0031:101-102 |
| `admin_identity_events`'in 90 günlük süpürmesinin (`identity_sweep_events`) **Go'da çağıranı yok**; kayıt taslaklarınınki saatte bir koşar | grep: `identity_sweep_events` hiçbir `_test.go` olmayan `.go` dosyasında ve `deploy/`, `scripts/`'te geçmiyor; `cmd/tappa/main.go:965-966` → T294 |

## 3. Hedef ve sınırlar

**Hedef.** Bağlı bir kimlik oturumu, kendi adresini başka bir adrese çevirebilir. Değişiklik **tek
transaction'da** kimliğe ve kişinin bütün işletmelerindeki aynaya yazılır; yeni adresin posta kutusu kanıtı
olmadan hiçbir şey değişmez; eski adres haber alır ve (karar K-E2) değişikliği geri alabilir.

**Değişmeyenler.**
- `tappa_app` kimlik tablolarında yetkisiz kalır ve `admin_users.email`'e yazamaz (ADR 0031:196-197).
  Yazan yalnız kapalı listenin yeni fonksiyonlarıdır; her biri kimliği bir **sırdan** bulur (§5.3).
- Bir istek her an tek tenant bağlamında koşar; BYPASSRLS gövdesinde tenant tablosuna dokunan her ifade açık
  `tenant_id` yüklemi taşır (CLAUDE.md §4.5 (3)).
- Audit satırları karşı işletmeyi, üyelik sayısını, `identity_id`'yi ve adresi anmaz (ADR 0031 §6).
- Bağlanmamış (eski, satır başına) bir satırın adresi bu yolla **değişmez** — kişi değildir; satır `tappa_app`'ın
  yetkisiyle de değişemez (00069:251). Contract'a kadar o satırlar kendi yollarında kalır.
- Operatör kimlik gücü almaz (ADR 0020; ADR 0031 K-12): değişikliği operatör yapamaz, geri alamaz.

## 4. Değerlendirilen seçenekler — değişikliğin nerede saklandığı

- **A — `admin_identity_verifications`'ı genişletmek** (önerilen): `purpose`'a `'change'` ve `'revert'`;
  `address citext NULL` (değişimde yeni adres, geri almada eski adres) ve `cancelled_at`. Tablo, var olan bir
  kimliğe postalanan linkin tek evi olarak kalır (00084'ün gerekçesi, `00084:8-14`); CLAUDE.md §4.5 (3)'ün
  "beş tablo" metni değişmez. Bedel: tablonun yorumundaki *"no column for a new address"* cümlesi ve 24 saat
  tavanı amaç başına olur (geri alma linki daha uzun yaşar — K-E3).
- **B — yeni bir tenant-kapsamsız tablo** (`admin_identity_email_changes`): daha okunaklı, ama altıncı kişi
  tablosu CLAUDE.md §4.5 (3) ve §6'nın tablo listesini, R5 muafiyetini, `pg-restore-verify` sayılarını ve
  katalog testlerini değiştirir. Kazanç yalnız okunaklılık. **Önerilmedi.**
- **C — yalnız aynayı güncelleyen bir operatör/bakım yazarı** (canlıdaki tek seferlik KM yazımının
  kalıcılaşması). Posta kutusu kanıtı, bildirim ve geri alma yok; operatör kimlik gücü alır (ADR 0020'yi
  bozar). **Reddedildi.**

## 5. Tasarım

### 5.1 Akış — üç adım

1. **İstek** (hesap sayfası, bağlı kimlik oturumu, POST): kişi yeni adresi ve **mevcut parolasını** yazar.
   Go parolayı `identity_password_for_session`'ın digest'iyle karşılaştırır (`changepassword.go:121-124`'ün
   deseni, iş kapısı altında) ve `identity_request_email_change`'i doğrulanmış digest'le çağırır; fonksiyon
   digest'i kimliğin kilidi altında yeniden denetler (2026-10-23 kararının emsali: araya giren bir sıfırlama
   eski parolanın isteğini düşürür — ADR 0031:26-30). Her istek **bir** değişim satırı ve **bir** mektup
   üretir; mektup yeni adrese gider: çakışma yoksa tek kullanımlık değişim linki, çakışma varsa (K-E5)
   linksiz bir bilgi mektubu. Yanıt her durumda aynıdır: *"Check your inbox at …"* (§5.4).
2. **Onay** (yeni adresin posta kutusu): link GET'te harcanmaz — değer kısa ömürlü çereze girer
   (`accountidentity.go:263-276`'nın deseni) ve tarayıcı hesap sayfasına gider; onay basışı (POST)
   `identity_change_email`'i link token'ının hash'i **ve** tıklayan tarayıcıdaki aynı kimliğin canlı oturumuyla
   çağırır (R2 — `identity_verify_email`'in kuralı, ADR 0031:324). Fonksiyon tek transaction'da kimliğin
   adresini, kişinin bütün bağlı üyeliklerindeki aynayı, oturumları, açık linkleri ve audit satırlarını yazar
   (§5.3, §5.5–§5.7).
3. **Bildirim ve geri alma** (eski adres): değişiklik commit edildikten sonra eski adrese **bir** bildirim
   gider (passwordnotice deseni: istek içinde, `SendExempt`, kendi tavanıyla); K-E2 (a) seçilirse bildirim
   tek kullanımlık bir **geri alma linki** taşır. Geri alma eski adresi geri koyar, parolayı kullanılamaz bir
   nöbetçi digest'e çevirir, her oturumu kapatır ve eski adrese bir kimlik sıfırlama linki açar (K-E11) —
   sıfırlamanın tüketimi `reset` kapsamlı onay penceresini zaten açar (ADR 0031:316).

### 5.2 Şema — `00101` (expand)

`admin_identity_verifications` (tenant-kapsamsız, sıcak değil):
- `purpose` CHECK'i `('verify','link','change','revert')` olur (ad korunur; 00084'ün `purpose_known`'u).
- `address citext NULL` + CHECK: `(address IS NOT NULL) = (purpose IN ('change','revert'))`. **Adres
  CHECK'le değil yüklemle doğrulanır** — bir CHECK ihlalinin `DETAIL`'i adresi log'a basardı (ADR 0031:261-265);
  6b kuralı (00076) her yazardan önce `IF (…) IS NOT TRUE THEN RETURN` olarak koşar.
- `cancelled_at timestamptz NULL` — kullanılmak ile iptal edilmek ayrı (00019'dan beri depo kuralı).
- TTL tavanı amaç başına: `'verify'`/`'link'`/`'change'` 24 saat (bugünkü), `'revert'` K-E3'ün süresi.
- Kimlik başına **en çok bir canlı `'change'` satırı** ve **en çok bir canlı `'revert'` satırı** —
  fonksiyonların kilit altındaki kuralı; bir kısmi UNIQUE `(identity_id) WHERE purpose = … AND used_at IS NULL
  AND cancelled_at IS NULL` süresi dolmuş satırı göremeyeceği için kullanılmaz (EM-7C'nin kimlik düzeyi
  karşılığı da kilitle yapıldı, ADR 0031:317).

`admin_identity_events_event_known` üç değer alır: `email_change_requested`, `email_changed`,
`email_reverted`; `detail` boş (`tenant` / `scope` anahtarları bu olaylarda kullanılmaz).

**Yetki eki** (fiil × sütun matrisi — ADR 0031 §5.2, kullanıcı onayı ister, K-E1):
`tappa_identitydefiner` → `admin_identities UPDATE (email)`; `admin_users UPDATE (email)`;
`admin_identity_verifications SELECT (address, cancelled_at)`, `INSERT (address)`, `UPDATE (cancelled_at)`;
`admin_identity_resets`'in var olan `UPDATE (cancelled_at)`'i ve `password_resets`'in var olan
`UPDATE (cancelled_at)`'i yeter. `tappa_app` ve `tappa_resolver` **hiçbir yeni yetki almaz**. `admin_users`
üzerindeki `GRANT` kendi `SET LOCAL lock_timeout`'uyla (`00072:271-276`'nın deseni); `admin_users` ALTER
edilmez.

### 5.3 Kapalı liste ekleri — `00102`

Üçü de sahibi `tappa_identitydefiner`, `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`,
`REVOKE ALL … FROM PUBLIC`, `EXECUTE` yalnız `tappa_app`; oturumla anahtarlı olanlar ADR 0035'in 12 saatlik
yüklemini taşır (`00085:425`). **Yeni adlandırılmış istisna yok:** üçü de kimliği bir sırdan bulur — canlı
oturum, değişim linki + canlı oturum, geri alma linki. §6.1'in *"canlı oturum, sıfırlama/davet/kayıt/doğrulama
token'ı"* cümlesi ikisini de kapsar, çünkü iki link de `admin_identity_verifications`'ın satırıdır (öneri: §6.1
ve CLAUDE.md §4.5 (3) metni değişmez; inceleyici geri alma linkinin adıyla sayılmasını isterse bu bir §6.1 +
CLAUDE.md değişikliğidir — K-E1).

| Fonksiyon | Anahtar | Ne yapar / ne zaman 0 satır |
|---|---|---|
| `identity_request_email_change(p_session_hash text, p_verified_digest text, p_link_hash text, p_new_email citext)` → `TABLE (change_id uuid, expires_at timestamptz)` | canlı oturum (+ kilit altında digest yeniden denetimi) | 6b yeni adreste; kimlik kilidi; oturum ve digest kilit altında; **ön koşullar** (K-E9): `email_verified_at IS NOT NULL`, onay penceresi kapalı, eski adreste bağsız aktif satır yok; yeni adres kimliğin kendi adresine citext'te eşit değil. Kimliğin canlı `'change'` satırını `cancelled_at` ile iptal eder, yenisini yazar (`address` = yeni adres, 24 saat), `email_change_requested` olayı. **Çakışmaya bakmaz** — çakışan adreste de satır yazılır (§5.4). Tenant tablosuna yalnız okur (oturum, üyelikler, eski adresteki satırlar — açık `tenant_id`), `audit_log`'a yazmaz (`identity_open_reset` emsali, ADR 0031:317) |
| `identity_change_email(p_link_hash text, p_session_hash text, p_revert_hash text)` → `TABLE (old_email text, new_email text, revoked_sessions integer, revert_expires_at timestamptz)` | **iki sır:** `'change'` linki + tıklayan tarayıcıda aynı kimliğin canlı oturumu | Ayrıntı aşağıda. Ret (bilinmeyen / iptal / harcanmış / süresi dolmuş link, başka kimliğin oturumu, yeni adres artık bir kimliğin, kişinin bir işletmesinde aynı adresli başka satır, ön koşulların biri): 0 satır, hiçbir şey yazılmaz. Dönüş: iki adres (bildirimin ve yanıtın alıcısı — 00075/00084'ün dönen adres emsali), bu işletmede kapanan oturum sayısı, geri alma linkinin bitişi (yazılmadıysa NULL) |
| `identity_revert_email_change(p_revert_hash text, p_sentinel_digest text)` → `TABLE (restored_email text, revoked_sessions integer)` | `'revert'` linki (tek sır; sıfırlama tüketiminin türü — ADR 0031:413-414) | Ayrıntı aşağıda. Ret (bilinmeyen / iptal / harcanmış / süresi dolmuş link, eski adres artık başka bir kimliğin): 0 satır |

**`identity_change_email`'in gövdesi — kilit sırası ve adımlar.**
1. Linki kilitsiz oku → yeni adresi al → **adresin danışma kilidi** (`00074:176`'nın anahtarı). Adres → kimlik
   sırası kayıt sahiplenmesinin parola koluyla aynıdır (ADR 0031:383-386); ters sırada bekleyen yazar yoktur.
2. Kimlik `FOR UPDATE` → kimliğin bütün üyelikleri `FOR UPDATE` (`identity_set_password`'ın sırası,
   `00085:446-448`) → link yeniden okunur (aynı kimlik, `'change'`, adres aynı, harcanmamış, iptal edilmemiş,
   süresi dolmamış) → oturum yeniden okunur (aynı kimlik, canlı, 12 saat).
3. Yeniden denetim: 6b; yeni adreste kimlik yok (`OPERATOR(public.=)`); kişinin aktif ya da devre dışı her
   işletmesinde yeni adresli başka satır yok (her ifadede açık `tenant_id`); K-E9'un ön koşulları hâlâ tutuyor.
4. `admin_identities SET email = yeni, email_verified_at = now()` (yeni posta kutusu kanıtlandı; pencere
   **açılmaz** — denetim el değiştirmedi: parola + oturum + yeni posta kutusu).
5. `FOR m IN` kimliğin `unlinked_at IS NULL` bütün bağlı satırları `ORDER BY tenant_id`: ayna
   (`WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity`); oturumlar (K-E6);
   o satırın canlı `password_resets` linkleri `cancelled_at` (§5.5); o işletmeye bir `admin.email.changed`
   satırı (§5.7).
6. Açık kimlik linkleri iptal (§5.5); link harcanır; `email_changed` olayı.
7. K-E2 (a): kimliğin canlı `'revert'` satırı **yoksa** bir tane yazılır (`address` = eski adres); varsa
   yazılmaz (§5.5, "zincir").
8. Çakışma kemeri: aynı adresi kilit almadan yazan bir yazar kalırsa `UNIQUE` ihlali (`23505`) gövdede
   yakalanır ve **0 satır** döner — yakalanmamış ihlal adresi `DETAIL`'e ve log'a basardı (ADR 0031:261-265).

**`identity_revert_email_change`'in gövdesi.** Link kilitsiz okunur → eski adresin danışma kilidi → kimlik ve
üyelikler `FOR UPDATE` → link yeniden (`'revert'`, harcanmamış, iptal edilmemiş, süresi dolmamış) → eski
adreste başka kimlik yok (yoksa 0 satır) → kimliğin `email` = eski adres, `email_verified_at = now()`,
`password_hash` = `p_sentinel_digest` (aşağıda) → bağlı her satır: ayna + digest kopyası (contract B'ye dek,
ADR 0031:227-229) + **bütün** oturumlar + canlı `password_resets` iptal + `admin.email.reverted` satırı →
kimliğin canlı `admin_identity_resets`, `'change'` ve öbür `'revert'` satırları iptal → link harcanır →
`email_reverted` olayı. **Nöbetçi digest:** çözmenin nöbetçi digest'iyle aynı tür (32 rastgele baytın bcrypt'i,
baytlar atılır, repoda sabit yok — ADR 0031:321). Gövde onu SQL'de pgcrypto ile üretebilir (00072'nin yolu,
`00072:146-148`); imzadaki `p_sentinel_digest` yalnız kart onu Go'da üretmeyi seçerse kalır — **kart karar
verir, imza katalog testinde pinlenir.** Geri alma oturum vermez; Go ardından eski adrese
`identity_open_reset` ile bir sıfırlama linki açar (K-E11), alıcı `identityRecipient` bağıyla.

### 5.4 Çakışma ve numaralandırma — düz zamanlama

İstek yanıtı, yeni adresin bir kimliğe ait olup olmadığını **söylememeli** (K-15'in dördüncü gereği).
Kanallar ve kapanışları:

| Kanal | Kapanış |
|---|---|
| Yanıt metni ve durum kodu | Her durumda aynı 303 + aynı flash (*"Check your inbox at …"* — kişinin yazdığı adres; K-E10) |
| Sayfanın sonraki hâli ("bekleyen değişim") | Bekleyen değişim **çizilmez**; çizilseydi bile her istekte bir satır yazıldığı için iki kol aynı görünürdü |
| Veritabanı işi | **Eşit iş:** iki kolda da aynı karşılaştırma (mevcut parola), aynı salt okuma çözümleyici (`resolve_identity_by_email(yeni)` — mektubu seçmek için; hiçbir şey yazmaz), aynı `identity_request_email_change` çağrısı (her kolda bir satır, bir olay) — girişin "eşit işten düz" ilkesi (ADR 0031:937-950) |
| Gönderim süresi | Mektup **istek yolundan çıkar**: sınırlı, ayrılmış bir kuyruğa bloklamadan sunulur (ADR 0022 §6'nın deseni; sıfırlama outbox'ı **paylaşılmaz** — anonim trafik onu doldurabilir, `passwordnotice.go:29-39`'in gerekçesi) ve yanıt sabit bir tabana bekler (sıfırlamanın 250 ms'si gibi, kendi sabiti, pinli) |
| Oran ve tavan redleri | Kişi kovası ve alıcı tavanı **karşılaştırmadan ve çözümlemeden önce** ayrılır (`TryCharge`, ADR 0031:441-447); iki kol aynı birimi harcar; tavanın reddi kuyruğa düşer, yanıtı değiştirmez |
| Onay anı (tıklama) | Linki yalnız yeni adresin posta kutusu alır; çakışma kolunda link postalanmaz (K-E5), dolayısıyla tıklanacak link yoktur. Tıklama anında adres **sonradan** alınmışsa tek ret sayfası (*"This link can't be used"*) — onu gören yeni posta kutusunun sahibidir |

**Kişinin kendi işletmesinde aynı adresli başka bir satır** (00006:96-97): bu durum istek anında `tappa_app`'a
görünmez (RLS; başka işletmelerin satırları), dolayısıyla link gider ve **onay** reddedilir. Reddi gören yine
kişinin kendisidir ve o satır kişinin kendi işletmesindedir — numaralandırma yoktur; adlandırılmış davranış.

**Yalnız harf büyüklüğü değişikliği** (`Ali@x` → `ali@x`): citext'te aynı adres; görünür bir ret
(*"That's already your address"*) — kişinin kendi bilgisi, numaralandırma değil (K-E13).

### 5.5 Açık linkler, davetler, talepler — değişiklik anında

| Açık şey | Ne olur | Neden |
|---|---|---|
| Kimliğin canlı sıfırlama linki (`admin_identity_resets`) | `cancelled_at` | Eski adrese gitti; **tek sırla** kimliği yazar (ADR 0031:316). Eski posta kutusu artık kişinin değilse kalıcı bir devralma yoluydu |
| Bağlı satırların canlı satır başına linkleri (`password_resets`) | `cancelled_at`, işletme başına açık `tenant_id` | `identity_consume_row_reset` bağlı satırın linkinden kimliği yazar (ADR 0031:315) — aynı gerekçe |
| Kimliğin öbür canlı `'change'` linkleri | `cancelled_at` | Kimlik başına tek canlı değişim |
| Kimliğin canlı `'verify'` / `'link'` linkleri | **dokunulmaz** | İki sır ister (link **ve** aynı kimliğin canlı oturumu, ADR 0031:324-325); eski posta kutusunun sahibi tek başına kullanamaz. `'link'`'in adayları kimliğin **o anki** adresinden okunur, yani değişimden sonra yeni adresin satırları — kişi o adresi az önce kanıtladı |
| Kimliğin canlı `'revert'` linki (önceki bir değişimden) | **dokunulmaz**, yeni `'revert'` yazılmaz | **Zincir:** saldırgan A→B, sonra B→C yapsa ve her değişim kendi geri alma linkini bassaydı, B'nin (saldırganın) linki kurbanın A geri almasını bozardı. Kural: kimlik başına tek canlı geri alma linki, en eskisi; yeni değişim onu ne iptal eder ne yenisini basar |
| Eski adrese açık davetler (`admin_invites`) | açık kalır; kabul 0 satır döner | `identity_accept_invite` `identity.email = invite.email` ister (ADR 0031:328); owner yeni adrese yeniden davet eder |
| Yeni adrese açık davetler | kabul edilebilir hâle gelir | Kimliğin adresi artık davetinkine eşit; tıklama yine aynı kimliğin oturumunu ister |
| Eski adresteki açık kayıt taslağı (`admin_signup_claims`) | "yeni adres" koluna düşer | Var olan kimlik kolu `identity.email = claim.email` ister (ADR 0031:331); tıklayan eski posta kutusunun sahibidir, onun kimliği doğar. Bu, geri almayı bloke eder (eski adres artık bir kimliğin) — adlandırılmış (§8) |
| Kişinin bağlanmamış eski satırları (eski adreste) | K-E9 (a): istek bunlar varken **reddedilir** | Değişimden sonra M14-22'nin bandı onları hiç göstermezdi (bant kimliğin adresinden okur, `identityaccount.go:557-595`) |
| Reddedilmiş satırlar (`unlinked_at`) | eski adreste kalır | Kişinin değildir; geri alma (Undo) adres eşitliği ister (ADR 0031:322) — K-E9 (a) pencere açıkken isteği reddettiği için çakışmaz |

### 5.6 Oturumlar (K-E6)

Öneri (a): onay basışını yapan oturum **kalır**, kişinin öbür bütün oturumları her işletmede kapanır —
`identity_set_password`'ın emsali (`00085:475-477`). Bir adres değişikliği hesabın kurtarma kanalını
değiştirir; parola değişimiyle aynı ağırlıktadır. Geri almada **bütün** oturumlar kapanır (devralma varsayımı).

### 5.7 Audit, olay, log

- **Audit** (tenant'a yazan fonksiyon o tenant'ın audit'ine yazar — ADR 0031:718-719): değişimde her bağlı
  üyeliğin işletmesine **bir** `admin.email.changed` satırı — canlıdaki tek seferlik yazımın adı
  (`state.md:807`) — `actor_id` = o işletmedeki üyelik satırı, `target` = aynı id, `detail`
  `{"outcome":"ok"}` ve yalnız sıfır değilse `revoked_other_sessions` (`identity_consume_reset`'in biçim dersi:
  bir 0 satırı "kimlik başına" diye ele verirdi — ADR 0031:494-497). Geri almada `admin.email.reverted`, aynı
  biçim. **Adres, eski/yeni ayrımı, karşı işletme, üyelik sayısı, `identity_id`, link id'si yok.** İstek
  audit'e yazmaz (hiçbir işletmenin değildir — ADR 0031:483-485).
- **Kimlik olayları:** `email_change_requested`, `email_changed`, `email_reverted`; `detail` boş.
- **Log:** ADR 0022 §10'un kapalı kümesi + `identity_id` (ADR 0031 §5.15); **yeni anahtar yok** — link id'si
  log'a girmez. Mektup sonuçları (gönderildi / teslim edilemedi / tavan / kesici) bütçeli log satırıdır, audit
  değil. Adres, token, hash, digest hiçbir satıra, hata metnine, panik değerine girmez (CLAUDE.md §7).

### 5.8 Oran sınırları

| Kova | Anahtar | Değer (öneri; sabitler pinli) |
|---|---|---|
| İstek | kişinin kovası (`AccountBucket`, yalnız bağlı oturum — bağlanmamış satır kişinin kovasını harcayamaz, ADR 0031:526-529) | saatte 5 (doğrulama linkiyle aynı, `adminratelimit.go:851-852`); karşılaştırmadan önce `TryCharge` |
| Yeni adrese mektup | `mail.RecipientCap`, kapsam = kişinin kovası (EM-7C'nin bölümlenmiş sayımı, `internal/mail/recipientcap.go:15-38`) | var olan saat/gün tavanı; kesici `Send` ile sayar ve reddedebilir |
| Eski adrese bildirim | kimlik başına | saatte 5 (`passwordNoticeLimit`'in emsali); kesici `SendExempt` ile sayar, reddetmez |
| Onay basışı | panelin oturum kovası (`adminSessionLimit`, `adminratelimit.go:623`) | yeni sınır yok — link tek kullanımlık ve oturum ister |
| Geri alma basışı | IP başına (`adminResetSubmitLimit`'in deseni, `adminresetlimits.go:194-195`) | 10 dk'da 10; token 256 bit |

### 5.9 Saklama — `00103`

Adres taşıyan satırlar (`'change'`, `'revert'`) kişisel veridir ve bugün bu tablonun hiçbir süpürmesi yoktur.
Öneri (K-E1'in parçası): `identity_sweep_events()` **aynı imzayla** yeniden tanımlanır ve ayrıca harcanmış,
iptal edilmiş ya da süresi dolmuş `'change'`/`'revert'` satırlarını en geç 1 gün sonra siler (definer'a bu
tabloda DELETE). Yeni bir süpürme fonksiyonu §6.1'in *"iki sabit periyotlu süpürme"* sayısını değiştirirdi;
genişletme değiştirmez. **Ön koşul:** süpürmenin bugün Go'da çağıranı yok (§2 son satır, T294) — EC-2 onu
`cmd/tappa/main.go`'ya taslak süpürmesinin yanına bağlar.

### 5.10 Bayrak ve yüzey

Yeni bir bayrak **yok**. Rotalar `mountGated` ile `SurfaceAccountIdentity`'nin altında bağlanır
(`internal/adminauth/flag.go:42`; bayrak kapalıyken panelin hiç sahip olmadığı yolun 404'ü, ADR 0031:623-627);
bayrak bugün `"on"` (`deploy/k8s/05-config.yaml:220`) ve contract A'da kalkar. Bağlanmamış satırın oturumu
formu görmez (§3).

## 6. Geçiş — expand/contract ve geri dönüş

- **Tek deploy:** 00101 + 00102 + 00103 + yeni binary. Migration Job rollout'tan önce koşar; eski pod yeni
  şemada çalışır, çünkü 00101 yalnız **ekler/genişletir**: yeni sütunlar `NULL`, `purpose` ve olay CHECK'leri
  genişler, eski binary'nin yazdığı `'verify'`/`'link'` satırları (adres `NULL`, 24 saat) yeni CHECK'lerden
  geçer; `identity_verify_email` ve `identity_link_by_email` değişmez. 00102 yalnız **yeni** fonksiyon yaratır.
  00103 `identity_sweep_events`'i aynı imzayla değiştirir; eski binary onu çağırmaz.
- **Geri dönüş = önceki imaj** (şema geride kalır, eski binary ona uyar). Down'lar ancak **sonraki** bir
  deploy'da, yeni fonksiyonları çağıran binary gittikten sonra; Down hiçbir şeyi önceki binary'nin çağırdığı
  şekilde düşürmez. 00101'in Down'ı `'change'`/`'revert'` satırları varken CHECK'i geri koyamaz: Down canlı
  (süresi dolmamış, harcanmamış) bir geri alma linki varken **sayıyla durur** (00076'nın Up'ındaki emsal) —
  geri alma hakkını sessizce silmez; süresi geçmişleri siler. Değiştirilmiş adresler veri olarak kalır.
- **Contract B ile ilişki (ADR 0031:1434-1450; `docs/plan/m14-tek-kimlik.md:966-973`):**
  `identity_revert_email_change` bağlı satırlara digest (nöbetçi) **kopyalar**, dolayısıyla M14-20b'nin 1b
  listesine girer (contract B'den sonra satıra `NULL`). `identity_change_email` digest'e dokunmaz; contract
  B'nin yeni tetikleyici kuralı (*"`email` yalnız `tappa_identitydefiner`"*) onunla uyumludur. EC-3 M14-20b
  kartına bu satırı ekler.
- **Contract A ile ilişki:** K-E9 (a)'nın "eski adreste bağsız satır" denetimi definer'ın içindedir
  (açık `tenant_id`, kimlikle sınırlı adlı e-posta-geniş okuma); Go tarafındaki ön kontrol M14-22'nin
  `HasBusinessToAdd`'ını kullanırsa M14-20a'nın o okumayı taşıma işine bağlanır (ADR 0031:595-597).

## 7. CLAUDE.md §4 ile ilişki

- **§4.5 (3):** kapalı listeye üç fonksiyon ve matrise `UPDATE (email)` — *"listeye fonksiyon eklemek … sütun
  matrisini … genişletmek kullanıcı onayı + ADR 0031 değişikliği ister"*. Bu ADR o onayı **ister** (K-E1);
  onaydan sonra EC-1 ADR 0031 §5.2'ye tarihli bir not ekler. Yeni adlandırılmış istisna yok (§5.3).
- **§4.6:** değişiklik bir kayıt silmez; audit satırları her işletmede yazılır; bildirim gönderilemezse
  değişiklik geri alınmaz, sonucu log'a yazılır (`passwordnotice.go:49-54`'nin ilkesi).
- **§7:** adres log'a ve audit'e girmez (§5.7).
- §4.1–§4.4 ve §4.7'ye dokunulmaz.

## 8. Kabul edilen riskler (adlandırılmış)

1. **Eski posta kutusunun sahibi, geri alma süresi boyunca hesabı geri alabilir** (K-E2 (a)). İşten ayrılan
   biri iş adresinden kişisel adrese geçerse eski işveren o süre içinde geri alabilir ve sıfırlama linkiyle
   kimliği — kişinin **başka** işletmelerindeki üyelikleri dahil — devralır. Hafifletme: süre (K-E3), geri
   almanın açtığı `reset` penceresi kişinin üyeliklerini yeni sahibine gösterir; kişi değişiklikten sonra
   bildirim mektubunu bilir. Bu, saldırganın değişikliğine karşı tek yolun bedelidir (operatör yardım
   edemez — ADR 0020).
2. **Eski adres bildirimi teslim edilemezse geri alma linki kaybolur.** Değişiklik durur (§4.6); iz log'dadır.
3. **Değişimden sonra eski adreste kayıt sahiplenmesi bir kimlik doğurursa geri alma reddedilir** (§5.5). Bu
   yalnız eski posta kutusunun sahibinin tıklamasıyla olur — yani geri almayı isteyecek kişinin kendisiyle.
4. **Kişinin kendi işletmesinde aynı adresli başka satır varsa** onay reddedilir ve ret yalnız kişiye görünür
   (§5.4).
5. **Bekleyen değişim sayfada görünmez** (§5.4) — kişi linki beklerken "ne oldu" diye bakamaz; ikinci istek
   öncekini iptal eder.

## 9. Reddedilen fikirler

- Operatörün adres değiştirmesi — ADR 0020, ADR 0031 K-12.
- Aynayı satır → kimlik eşitleyen bir tetikleyiciyle güncellemek — ADR 0031 §11 (tenant A'nın yazması B'ye taşınır).
- Çakışmada açık bir *"bu adres kullanımda"* hatası — K-15'in dördüncü gereği (numaralandırma).
- Değişimi yalnız parolayla (posta kutusu kanıtı olmadan) yapmak — K-15'in ikinci gereği; kurtarma kanalını
  kanıtsız bir adrese çevirirdi.
- Değişim linkini oturumsuz harcamak — R2 (ADR 0031:324): link tek başına bir kimliği yazamaz.

## 10. Açık kararlar — kullanıcının (öneriyle)

| # | Soru | Seçenekler | Öneri |
|---|---|---|---|
| **K-E1** | Kapalı liste ve matris eki | (a) §5.2–§5.3 ve §5.9: üç fonksiyon, `admin_identity_verifications`'a iki amaç + iki sütun, definer'a `UPDATE (email)` iki tabloda, üç olay, süpürmenin genişlemesi; §6.1 / CLAUDE.md metni değişmez · (b) ayrı tablo (CLAUDE.md §4.5 (3) + §6 metni değişir) · (c) hiç yapma | **(a)** |
| **K-E2** | "Bu ben değilim" yolu | (a) eski adrese bildirim + tek kullanımlık geri alma linki · (b) yalnız bildirim (geri dönüş yok; operatör de yardım edemez) · (c) bekleme süreli değişim: değişim N gün sonra yürürlüğe girer, eski adreste iptal linki | **(a)** — ana akımın karşılığı: Google/Microsoft eski adrese "bu sen değilsen hesabını güvene al" bağlantısı gönderir |
| **K-E3** | Geri alma linkinin ömrü | (a) 24 saat (tablonun bugünkü tavanı) · (b) 72 saat · (c) 7 gün | **(c) 7 gün** — hafta sonu okunmayan bir kutu; bedeli §8 risk 1 |
| **K-E4** | Değişim linkinin ömrü | (a) 24 saat · (b) 1 saat | **(a)** — doğrulama linkiyle aynı; iki sır ister |
| **K-E5** | Çakışmada yeni adrese ne gider | (a) linksiz bilgi mektubu (*"Someone asked to use this address for a Taptime sign-in. It already has one, so nothing changed."*) · (b) hiçbir şey | **(a)** — iki kol aynı mektup sayısını öder; adresin gerçek sahibine haber verir |
| **K-E6** | Değişimden sonra oturumlar | (a) onaylayan oturum kalır, öbürleri her işletmede kapanır · (b) hepsi kapanır (yeniden giriş) · (c) hiçbiri | **(a)** — parola değişiminin emsali |
| **K-E7** | Eski adrese istek anında da haber | (a) yalnız değişim anında · (b) istek anında da | **(a)** — istek tek başına hiçbir şeyi değiştirmez |
| **K-E8** | Eski adres bildirimi yeni adresi anar mı | (a) anmaz · (b) maskeli anar · (c) tam anar | **(a)** — eski kutunun sahibi artık başkası olabilir (§8 risk 1); kişinin yeni adresi ona söylenmez |
| **K-E9** | İsteğin ön koşulları | (a) mevcut adres doğrulanmış **ve** onay penceresi kapalı **ve** eski adreste bağsız aktif işletme yok · (b) yalnız doğrulanmış adres · (c) hiçbiri | **(a)** — doğrulanmamış adres değişirse `first_verify` penceresi hiç açılmaz (ADR 0031 §5.8) ve bildirim kanıtlanmamış bir kutuya gider; pencere açıkken Undo kırılır; bağsız satırlar banttan düşer |
| **K-E10** | Kullanıcıya görünen metinler | EC-2'nin taslakları: form başlığı *"Change your sign-in email"*; yanıt *"Check your inbox at {adres}. The change happens when you open the link there."*; onay *"Change your sign-in email to the address this link was sent to?"*; ret *"This link can't be used. Ask for a new one from your account page."*; eski adres mektubu *"Your Taptime sign-in email was changed on {tarih}. If this wasn't you, press the button below within 7 days to undo it."*; geri alma *"Your sign-in email is back. We sent a link to set a new password."*; ön koşul *"Confirm your current email first."* | Kullanıcı onaylar ya da düzeltir |
| **K-E11** | Geri almadan sonra | (a) eski adrese otomatik bir sıfırlama linki · (b) kişi "Forgot password"u kendisi kullanır | **(a)** — bir adım eksik, aynı güvenlik (link yine eski posta kutusuna) |
| **K-E12** | Ne zaman | (a) şimdi, contract'tan önce, §5.5'in kurallarıyla · (b) contract B'den sonra (eski kol yok, satır başı link yok) | **(a)** — contract A en erken 2026-12-27 ve tek bağsız owner kaldıkça ertelenir (`state.md:807`); ek kurallar küçük |
| **K-E13** | Yalnız harf büyüklüğü değişikliği | (a) aynı adres sayılır, reddedilir · (b) izin verilir (yazım düzeltmesi) | **(a)** — citext'te eşit; yazım isteği nadir |

## 11. Testler (özü — adlar yapıcının; henüz var olmayan test burada adlandırılmaz)

- **Yetki ve katalog:** imza tablosu, fiil × sütun matrisi ve "her yetki kullanılıyor" egzersizi üç fonksiyonu ve
  yeni hücreleri kapsar (`TestIdentityCatalog_TheFunctionsAndTheirExactSignatures`,
  `TestIdentityDefiner_ColumnMatrix`, `TestIdentityDefiner_EveryGrantIsUsed`); `tappa_app`, `tappa_operator`,
  `tappa_opdefiner` hâlâ `admin_users.email`'e yazamaz ve kimlik tablolarında yetkisiz
  (`TestIdentityFunctions_NoOperatorRoleMayExecuteThem`); `privCatalogAllowed` güncellenir.
- **Kemer:** üç gövde kimlik kemerinde, aile başına sayıyla (`identityBeltFamilies` deseni); her tenant-tablo
  ifadesi açık `tenant_id`; adres kuralı her yazarda (`TestIdentityAddressRule_EveryIdentityInsertDecidesIt`'in
  kapsamı `UPDATE … SET email`'e genişler).
- **Davranış (gerçek Postgres):** 1, 2 ve 10 işletmeli kimlikte değişimden sonra ayna = kimlik adresi her
  satırda; tetikleyici `tappa_app`'ın ve `tappa_owner`'ın doğrudan yazmasını hâlâ reddeder; her ret dalı 0 satır
  ve hiçbir yazı; eşzamanlı iki onay → tam bir başarı, 500 yok (`-race`, N goroutine); aynı adrese eşzamanlı
  değişim + kayıt sahiplenmesi + davet kabulü → biri kazanır, 500 yok, `DETAIL`'de adres yok; zincir (A→B→C,
  iki geri alma) → yalnız en eski link geri alır.
- **Düz zamanlama (HTTP, gerçek Postgres):** çakışmasız / kimliğe çakışan / kendi adresi olmayan üç kol iç içe;
  her yanıt tabanı öder, medyanlar aynı bantta (`TestPanelIdentityReset_TimingIsFlatOverHTTP`'nin biçimi).
- **Log/audit sızıntısı:** röle davranışlarının hepsinde log anahtarları ADR 0022 §10 + `identity_id`; hiçbir
  satırda adres (her harf büyüklüğünde), token, hash; audit satırında yalnız `outcome` ve sıfır değilse sayı
  (`TestPasswordNotice_LogsOnlyIdsClassAndCode`'un biçimi).
- **Mutasyon kanıtları:** her yeni koruma için (yüklemi/predikatı kaldır → kırmızı → geri al) — liste kartta.
