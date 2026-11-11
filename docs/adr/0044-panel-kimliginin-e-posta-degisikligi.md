# ADR 0044 — Panel kimliğinin e-posta adresini değiştirmek: yeni adresin posta kutusu kanıtı, eski adrese bildirim ve geri alma, aynanın definer'la güncellenmesi

- **Durum:** **Önerildi (2026-11-11)** — kart yazıldı, iki bağımsız inceleyicinin onayı ve kullanıcının §10'daki
  kararları bekleniyor. Kod, migration ve CLAUDE.md değişikliği **yok**. Kullanıcı kararı (2026-11-11):
  *"parti 7'de kartını yaz"*; önce kart, iki bağımsız onay, **sonra** kod.
  **Denetim turu 1 (2026-11-11, 87e2ed4c üzerinde):** üçüncü göz ve güvenlik **RET**. Düzeltilenler: geri
  almada aynı-işletme çakışması ve bütün gövdeyi saran `unique_violation` (YÜKSEK-1, §5.3, §8 risk 8); Down'ın
  olay satırlarıyla durması (§5.2, §6); zincir açığı ve K-E9 (d) (§5.5, §8 risk 6); flash'ın adres taşıyamaması
  (§5.4, K-E10 (i)); K-E1'in gerçek kapsamı — kemerin dört eki, "silme yok" istisnası (§5.9, K-E1); alıcı
  tavanının ne zaman sayıldığı (§5.4, §5.8); `identity_open_reset`'in alıcı yarışı (S-1, §5.3); oturum ve zincir
  alanının yeniden incelemesi (S-2, §5.6); sessizce verilmiş iki karar K-E1 (iii) ve K-E14 olarak; K-E15.
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
   linksiz bir bilgi mektubu. Yanıt her durumda aynıdır: *"Check your inbox at the new address"* — flash panelin
   kapalı sözcük listesindendir (`internal/handler/accountactions.go:311-319`), adres taşımaz (§5.4, K-E10 (i)).
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
   sıfırlamanın tüketimi `reset` kapsamlı onay penceresini zaten açar (ADR 0031:316). Değişimde geri alma
   linki yazılmadıysa (`revert_expires_at` NULL — K-E9 (d) seçilmezse zincirin ikinci adımı, §5.5) bildirim
   **linksiz** biçimdedir (K-E10 (vi)'nın ikinci metni); "within 7 days" cümlesi yalnız linkli biçimde geçer.
4. **Yeni adrese onay mektubu** (K-E14): öneri (a) **yok** — yeni posta kutusunun sahibi linki az önce açtı ve
   sayfanın cevabını gördü. (b) seçilirse: değişimin commit'inden sonra, bildirimle aynı yoldan (istek içinde,
   `SendExempt`, kimlik başına aynı tavanı paylaşır), linksiz, K-E10 (vii)'nın metniyle.

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

**00101'in Down'ı iki koşulda sabit bir mesajla durur, hiçbir şey değiştirmez** (00079'un Down'ının emsali,
`00079:471-476`): (1) `admin_identity_events`'te `email_*` olayı varken — olay CHECK'i 00079'un biçimine geri
konamaz ve olay satırı silinemez (R17: tek silme yolu 90 günlük süpürmedir); (2) canlı bir `'revert'` satırı
varken (§6). Mesaj satır sayısını da verebilir (00076'nın Up'ı gibi); adres vermez.

**Yetki eki** (fiil × sütun matrisi — ADR 0031 §5.2, kullanıcı onayı ister, K-E1):
`tappa_identitydefiner` → `admin_identities UPDATE (email)`; `admin_users UPDATE (email)`;
`admin_identity_verifications SELECT (address, cancelled_at)`, `INSERT (address)`, `UPDATE (cancelled_at)`
ve — yalnız K-E1'in saklama seçeneği (a) ise — `DELETE` (00103'ün süpürmesi, §5.9);
`admin_identity_resets`'in var olan `UPDATE (cancelled_at)`'i ve `password_resets`'in var olan
`UPDATE (cancelled_at)`'i yeter. `tappa_app` ve `tappa_resolver` **hiçbir yeni yetki almaz**. `admin_users`
üzerindeki `GRANT` kendi `SET LOCAL lock_timeout`'uyla (`00072:271-276`'nın deseni); `admin_users` ALTER
edilmez.

### 5.3 Kapalı liste ekleri — `00102`

Üçü de sahibi `tappa_identitydefiner`, `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`,
`REVOKE ALL … FROM PUBLIC`, `EXECUTE` yalnız `tappa_app`; oturumla anahtarlı olanlar ADR 0035'in 12 saatlik
yüklemini taşır (`00085:425`). **Yeni adlandırılmış istisna yok:** üçü de kimliği bir sırdan bulur — canlı
oturum, değişim linki + canlı oturum, geri alma linki. **Ama geri alma linki sıfırlama sınıfındadır:** tek sırla
kimliğin adresini **ve** parolasını yazar. §6.1'in *"doğrulama token'ı"* sözcüğüne sığdırmak (iki link de
`admin_identity_verifications`'ın satırıdır) teknik olarak doğru, anlamca zorlamadır; bu yüzden K-E1 (ii) onu
§6.1 ve CLAUDE.md §4.5 (3)'te **adıyla saymayı** önerir (*"… doğrulama token'ı; e-posta değişikliğinin geri
alma linki"* — iki metin bayt-aynı, `TestClaudeMD_TheIdentityExceptionIsADR0031sSection61`).

**Dördüncü değişiklik — `identity_open_reset`'in alıcı bağı (S-1).** Bugün fonksiyon yalnız kimlik id'siyle
anahtarlıdır (`00072:1236-1277`) ve alıcı adres ondan **önce**, Go'da çözülür (`identityreset.go:31-44`). Bir
değişim ya da geri alma ikisinin arasına düşerse link, kimliğin **artık bırakmış olduğu** adrese gider: eski
posta kutusunun sahibi, meşru bir değişimden sonra tek sırla kimliği devralırdı. En küçük kapanış iki parçalıdır,
ikisi de gerekli:
- `identity_open_reset` dördüncü bir argüman alır, `p_recipient text DEFAULT NULL` (imza değişir → `DROP` +
  `CREATE`, T5 kuralı aynen; eski binary'nin üç argümanlı çağrısı `DEFAULT` ile çözülür — 00080/00084 emsali).
  Kendi danışma kilidinden (`'admin-identity-reset:' || id`, `00072:1253`) sonra `i.email::text =
  p_recipient` değilse **0 satır** (hiçbir şey basılmaz). Go her çağrıda `identityRecipient`'ın adresini geçer
  (`recipientOf`'un tek satırı); 0 satır "kimlik yok" ile aynı cevabı alır.
- `identity_change_email` ve `identity_revert_email_change` aynı `'admin-identity-reset:'` kilidini **adres
  kilidinden sonra, kimlik satır kilidinden önce** alır. Böylece bir açılış ya değişimden önce commit eder
  (değişim onun linkini §5.5 gereği iptal eder) ya da değişimden sonra koşar ve yeni adresi görür (0 satır).
  Sıra şart: `identity_open_reset`'in INSERT'inin FK denetimi kimlik satırında `FOR KEY SHARE` alır; değişim
  kimliği `FOR UPDATE` tutup sıfırlama kilidini bekleseydi iki taraf kilitlenirdi.
- Go'da yalnız gönderim anında yeniden okumak **yetmez** (okuma ile gönderim arasında aynı yarış).
- Rollout dakikalarında eski binary üç argümanla çağırır, denetim yoktur — adlandırılmış bedel (§8).
- Doğrulama ve bağlama linklerinin alıcısı da önceden çözülür (`identityaccount.go:184-219`), ama ikisi de iki
  sır ister (link + aynı kimliğin canlı oturumu); eski posta kutusu tek başına kullanamaz — kapsam dışı.

| Fonksiyon | Anahtar | Ne yapar / ne zaman 0 satır |
|---|---|---|
| `identity_request_email_change(p_session_hash text, p_verified_digest text, p_link_hash text, p_new_email citext)` → `TABLE (change_id uuid, expires_at timestamptz)` | canlı oturum (+ kilit altında digest yeniden denetimi) | 6b yeni adreste; kimlik kilidi; oturum ve digest kilit altında; **ön koşullar** (K-E9): `email_verified_at IS NOT NULL`, onay penceresi kapalı, eski adreste bağsız aktif satır yok, (K-E9 (d) seçilirse) kimliğin canlı `'revert'` satırı yok; yeni adres kimliğin kendi adresine citext'te eşit değil. Kimliğin canlı `'change'` satırını `cancelled_at` ile iptal eder, yenisini yazar (`address` = yeni adres, 24 saat), `email_change_requested` olayı. **Çakışmaya bakmaz** — çakışan adreste de satır yazılır (§5.4). Tenant tablosuna yalnız okur, `audit_log`'a yazmaz (`identity_open_reset` emsali, ADR 0031:317). Okumalar kemerin sınıflarıyla: oturum araması (`identitySessionKeyLookups`), üyelikler (kimlikle sınırlı, `identityTenantWideReads`), **eski adresteki bağsız satır** kimliğin kendi adresiyle anahtarlı **adlı e-posta-geniş okumadır** (`identityEmailWideReads`, `internal/db/identitybelt_test.go:142`; `identity_confirm_window` / `identity_link_by_email` emsali) — tenant'ı önceden bilinmediği için açık `tenant_id` yüklemi **taşıyamaz**, sınırı kimliğin adresidir |
| `identity_change_email(p_link_hash text, p_session_hash text, p_revert_hash text)` → `TABLE (old_email text, new_email text, revoked_sessions integer, revert_expires_at timestamptz)` | **iki sır:** `'change'` linki + tıklayan tarayıcıda aynı kimliğin canlı oturumu | Ayrıntı aşağıda. Ret (bilinmeyen / iptal / harcanmış / süresi dolmuş link, başka kimliğin oturumu, yeni adres artık bir kimliğin, kişinin bir işletmesinde aynı adresli başka satır, ön koşulların biri): 0 satır, hiçbir şey yazılmaz. Dönüş: iki adres (bildirimin ve yanıtın alıcısı — 00075/00084'ün dönen adres emsali), bu işletmede kapanan oturum sayısı, geri alma linkinin bitişi (yazılmadıysa NULL) |
| `identity_revert_email_change(p_revert_hash text, p_sentinel_digest text)` → `TABLE (restored_email text, revoked_sessions integer)` | `'revert'` linki (tek sır; sıfırlama tüketiminin türü — ADR 0031:413-414) | Ayrıntı aşağıda. Ret (bilinmeyen / iptal / harcanmış / süresi dolmuş link, eski adres artık başka bir kimliğin): 0 satır |

**`identity_change_email`'in gövdesi — kilit sırası ve adımlar.**
1. Linki kilitsiz oku → yeni adresi al → **adresin danışma kilidi** (`00074:176`'nın anahtarı) → kimliğin
   **sıfırlama danışma kilidi** (`00072:1253`'ün anahtarı; S-1, yukarıda). Adres → kimlik sırası kayıt
   sahiplenmesinin parola koluyla aynıdır (ADR 0031:383-386); ters sırada bekleyen yazar yoktur.
2. Kimlik `FOR UPDATE` → kimliğin bütün üyelikleri `FOR UPDATE` (`identity_set_password`'ın sırası,
   `00085:446-448`) → link yeniden okunur (aynı kimlik, `'change'`, adres aynı, harcanmamış, iptal edilmemiş,
   süresi dolmamış) → oturum yeniden okunur (aynı kimlik, canlı, 12 saat).
3. Yeniden denetim: 6b; yeni adreste kimlik yok (`OPERATOR(public.=)` — `admin_identities`'i **adresle**
   anahtarlayan okuma: kimlik-anahtar kuralının dördüncü sınıfı, `identityClaimAddressKeys`'in emsali,
   `identitybelt_test.go:160-167`; adresi linkin satırı — bir sır — verdi); kişinin aktif ya da devre dışı her
   işletmesinde yeni adresli başka satır yok (üyelik döngüsünde, her ifadede açık `tenant_id`); K-E9'un ön
   koşulları hâlâ tutuyor.
4. `admin_identities SET email = yeni, email_verified_at = now()` (yeni posta kutusu kanıtlandı; pencere
   **açılmaz** — denetim el değiştirmedi: parola + oturum + yeni posta kutusu).
5. `FOR m IN` kimliğin `unlinked_at IS NULL` bütün bağlı satırları `ORDER BY tenant_id`: ayna
   (`WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity`); oturumlar (K-E6);
   o satırın canlı `password_resets` linkleri `cancelled_at` (§5.5); o işletmeye bir `admin.email.changed`
   satırı (§5.7).
6. Açık kimlik linkleri iptal (§5.5); link harcanır; `email_changed` olayı.
7. K-E2 (a): bir `'revert'` satırı yazılır (`address` = eski adres). K-E9 (d) seçilirse canlı bir geri alma
   linki varken istek zaten reddedilmiştir, burada her zaman yazılır; (d) seçilmezse kimliğin canlı `'revert'`
   satırı **varken yazılmaz** (§5.5, "zincir" — ve onun açığı, §8 risk 6).
8. Çakışma kemeri: **bütün gövde** tek bir `EXCEPTION WHEN unique_violation THEN RETURN` bloğundadır
   (`00084:430-434`'ün emsali) — iç bir blok, kendisinden önceki yazıları bırakırdı; fonksiyon düzeyindeki blok
   bir alt transaction'dır ve hepsini geri alır. Aynı adresi kilit almadan yazan bir yazar kalırsa ihlal **0
   satır** olur; yakalanmamış ihlal adresi `DETAIL`'e ve log'a basardı (ADR 0031:261-265).

**`identity_revert_email_change`'in gövdesi.** Bütün gövde `identity_change_email`'inki gibi tek bir
`EXCEPTION WHEN unique_violation THEN RETURN` bloğundadır (adım 8). Link kilitsiz okunur → eski adresin danışma
kilidi → kimliğin sıfırlama danışma kilidi (S-1) → kimlik ve üyelikler `FOR UPDATE` → link yeniden (`'revert'`,
harcanmamış, iptal edilmemiş, süresi dolmamış) → eski adreste başka kimlik yok (adresle anahtarlı dördüncü sınıf
okuma; varsa 0 satır) → **kişinin bağlı her işletmesinde eski adresli başka satır yok** (üyelik döngüsünde açık
`tenant_id`; varsa 0 satır — YÜKSEK-1: kişinin o işletmede bir zamanlar **reddettiği** satır eski adreste kalır,
çünkü çözme `identity_id`'yi NULL yapar ama adrese dokunmaz, `00072:903-905`; kişi değişimden sonra aynı işletmeye
yeni adresle davetle dönerse aynayı geri çevirmek `admin_users_tenant_email_key`'e, `00006:96-97`, çarpardı)
→ kimliğin `email` = eski adres, `email_verified_at = now()`,
`password_hash` = `p_sentinel_digest` (aşağıda) → bağlı her satır: ayna + digest kopyası (contract B'ye dek,
ADR 0031:227-229) + **bütün** oturumlar + canlı `password_resets` iptal + `admin.email.reverted` satırı →
kimliğin canlı `admin_identity_resets`, `'change'` ve öbür `'revert'` satırları iptal → link harcanır →
`email_reverted` olayı. **Nöbetçi digest:** çözmenin nöbetçi digest'iyle aynı tür (32 rastgele baytın bcrypt'i,
baytlar atılır, repoda sabit yok — ADR 0031:321). Gövde onu SQL'de pgcrypto ile üretebilir (00072'nin yolu,
`00072:146-148`); imzadaki `p_sentinel_digest` yalnız kart onu Go'da üretmeyi seçerse kalır — **kart karar
verir, imza katalog testinde pinlenir.** Geri alma oturum vermez; Go ardından eski adrese
`identity_open_reset` ile bir sıfırlama linki açar (K-E11), `p_recipient` = geri almanın döndürdüğü adres (S-1).

**Geri almanın rotası** oturum istemez: `mountGated(SurfaceAccountIdentity)` altında ama panelin oturum isteyen
grubunun **dışında**, sıfırlama ve davet sayfaları gibi kendi bağlamasıyla (ADR 0031:692-693'ün davet emsali;
iniş GET'i çereze alır, onay POST'u Origin denetimli). Askıdaki bir işletme geri almayı engellemez (bir güvenlik
eylemidir); EC-2 bunu ADR 0025'in istisna listesine karşı doğrular ve gerekiyorsa listeyi adıyla genişletir —
o bir kullanıcı kararıdır (K-E15).

### 5.4 Çakışma ve numaralandırma — düz zamanlama

İstek yanıtı, yeni adresin bir kimliğe ait olup olmadığını **söylememeli** (K-15'in dördüncü gereği).
Kanallar ve kapanışları:

| Kanal | Kapanış |
|---|---|
| Yanıt metni ve durum kodu | Her durumda aynı 303 + aynı flash. Flash panelin kapalı sözcüklü sorgu parametresidir (`accountactions.go:311-319`); adresi URL'de taşımak onu ingress log'una, tarayıcı geçmişine ve `Referer`'a koyardı (CLAUDE.md §7), bekleyen satırdan okumak bu tablonun ikinci satırıyla çelişirdi. Öneri K-E10 (i) (a): metin adres anmaz — *"Check your inbox at the new address"* |
| Sayfanın sonraki hâli ("bekleyen değişim") | Bekleyen değişim **çizilmez**; çizilseydi bile her istekte bir satır yazıldığı için iki kol aynı görünürdü |
| Veritabanı işi | **Eşit iş:** iki kolda da aynı karşılaştırma (mevcut parola), aynı salt okuma çözümleyici (`resolve_identity_by_email(yeni)` — mektubu seçmek için; hiçbir şey yazmaz), aynı `identity_request_email_change` çağrısı (her kolda bir satır, bir olay) — girişin "eşit işten düz" ilkesi (ADR 0031:937-950) |
| Gönderim süresi | Mektup **istek yolundan çıkar**: sınırlı, ayrılmış bir kuyruğa bloklamadan sunulur (ADR 0022 §6'nın deseni; sıfırlama outbox'ı **paylaşılmaz** — anonim trafik onu doldurabilir, `passwordnotice.go:29-39`'in gerekçesi) ve yanıt sabit bir tabana bekler (sıfırlamanın 250 ms'si gibi, kendi sabiti, pinli) |
| Oran ve tavan redleri | Yalnız **kişinin kovası** karşılaştırmadan ve çözümlemeden önce ayrılır (`TryCharge`, ADR 0031:441-447); iki kol aynı birimi harcar. Alıcı tavanının önceden ayırma yolu yoktur — `mail.RecipientCap` yalnız `Capped` ve `Send` taşır (`internal/mail/recipientcap.go:263`, `:278`) — tavan kuyruktaki işçinin `Send`'inde sayılır ve reddi orada, yanıttan sonra olur; yanıtı değiştirmez |
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
| Kimliğin canlı `'revert'` linki (önceki bir değişimden) | **Öneri K-E9 (d):** böyle bir link varken yeni değişim **isteği reddedilir** — geri alma penceresi başına tek değişim, zincir hiç oluşmaz. (d) seçilmezse: link **dokunulmaz**, yeni `'revert'` yazılmaz | **Zincir:** saldırgan A→B, sonra B→C yapsa ve her değişim kendi geri alma linkini bassaydı, B'nin (saldırganın) linki kurbanın A geri almasını bozardı; "en eski link" kuralı bunu kapatır ama §8 risk 6'yı açık bırakır (kişinin kendi önceki A0→A değişiminden sonra gelen bir saldırıda A'nın sahibinin geri dönüşü yoktur). (d) ikisini birden kapatır |
| Eski adrese açık davetler (`admin_invites`) | açık kalır; kabul 0 satır döner | `identity_accept_invite` `identity.email = invite.email` ister (ADR 0031:328); owner yeni adrese yeniden davet eder |
| Yeni adrese açık davetler | kabul edilebilir hâle gelir | Kimliğin adresi artık davetinkine eşit; tıklama yine aynı kimliğin oturumunu ister |
| Eski adresteki açık kayıt taslağı (`admin_signup_claims`) | "yeni adres" koluna düşer | Var olan kimlik kolu `identity.email = claim.email` ister (ADR 0031:331); tıklayan eski posta kutusunun sahibidir, onun kimliği doğar. Bu, geri almayı bloke eder (eski adres artık bir kimliğin) — adlandırılmış (§8) |
| Kişinin bağlanmamış eski satırları (eski adreste) | K-E9 (a): istek bunlar varken **reddedilir** | Değişimden sonra M14-22'nin bandı onları hiç göstermezdi (bant kimliğin adresinden okur, `identityaccount.go:557-595`) |
| Reddedilmiş satırlar (`unlinked_at`) | eski adreste kalır | Kişinin değildir; geri alma (Undo) adres eşitliği ister (ADR 0031:322) — K-E9 (a) pencere açıkken isteği reddettiği için çakışmaz |

### 5.6 Oturumlar (K-E6)

Öneri (a): onay basışını yapan oturum **kalır**, kişinin öbür bütün oturumları her işletmede kapanır —
`identity_set_password`'ın emsali (`00085:475-477`). Bir adres değişikliği hesabın kurtarma kanalını
değiştirir; parola değişimiyle aynı ağırlıktadır. Geri almada **bütün** oturumlar kapanır (devralma varsayımı).

**S-2 incelemesi — oturum ve zincir alanında devralma boşluğu (2026-11-11, bu turda yeniden okundu).**
- *Saldırganın oturumu geri almadan sağ çıkar mı?* Geri alma kimliğin `unlinked_at IS NULL` her bağlı satırının
  **bütün** oturumlarını kapatır ve parolayı nöbetçi digest yapar. Oturum doğuran üç yol kimliği `FOR UPDATE`
  kilitleyip kilit altında yeniden denetler: geçiş eski oturumu yeniden okur (`00085:211-227`), giriş digest'i
  `p_verified_digest`'e karşı denetler (`00071:189-249`; nöbetçiden sonra hiçbir parola tutmaz), satır oturumu
  güncel digest'i ister (ADR 0036). Geri alma aynı kimlik kilidini aldığı için onlarla sıralanır: önce commit eden
  geçiş/giriş oturumu geri alma tarafından kapatılır, sonra gelen 0 satır alır. **Bulgu yok; kart bunu bir
  eşzamanlılık testine bağlar** (geri alma ↔ geçiş / giriş / `identity_link_by_email`, N goroutine, sonunda canlı
  oturum 0).
- *Saldırganın bastığı linkler?* Kimlik sıfırlaması ve satır başı sıfırlamalar geri almada iptal (§5.3);
  `'change'` iptal; `'verify'`/`'link'` iki sır ister ve bütün oturumlar kapalıdır.
- *Saldırganın pencerede kurduğu üyelikler* (ör. kendi işletmesine kurbanın kimliğiyle davet kabulü): geri almadan
  sonra durur; K-E11 (a)'nın sıfırlama linkinin tüketimi `reset` kapsamlı pencereyi açar ve `invite` dahil
  pencereden önce kurulmuş her bağı listeler (ADR 0031:321), kişi orada çözer. Adlandırılmış (§8 risk 7).
- *Saldırganın onay oturumu değişimden sağ çıkar* (K-E6 (a) onaylayanı tutar) — tasarım gereği; geri alma onu
  kapatır. K-E2 (b) seçilirse saldırganın oturumu 12 saatlik ömrüne dek yaşar (ADR 0035).
- *"En eski link" kuralı:* **açık bulundu** — ORTA-2'nin senaryosu (§8 risk 6). Öneri K-E9 (d).
- *Sıfırlama linkinin alıcı yarışı:* S-1, §5.3'ün dördüncü değişikliğiyle kapandı.

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
| Yeni adrese değişim/çakışma mektubu | `mail.RecipientCap`, kapsam = kişinin kovası (EM-7C'nin bölümlenmiş sayımı, `internal/mail/recipientcap.go:15-38`) | var olan saat/gün tavanı; **kuyruktaki işçinin `Send`'inde** sayılır (önceden ayırma yok — §5.4); kesici `Send` ile sayar ve reddedebilir |
| Eski adrese bildirim (+ K-E14 (b) ise yeni adrese onay) | kimlik başına, ikisi tek tavanı paylaşır | saatte 5 (`passwordNoticeLimit`'in emsali); kesici `SendExempt` ile sayar, reddetmez |
| Onay basışı | panelin oturum kovası (`adminSessionLimit`, `adminratelimit.go:623`) | yeni sınır yok — link tek kullanımlık ve oturum ister |
| Geri alma basışı | IP başına (`adminResetSubmitLimit`'in deseni, `adminresetlimits.go:194-195`) | 10 dk'da 10; token 256 bit |

### 5.9 Saklama — `00103`

Adres taşıyan satırlar (`'change'`, `'revert'`) kişisel veridir ve bugün bu tablonun hiçbir süpürmesi yoktur.
Seçenekler **K-E1 (iii)**'tür (saklama süresi de bir karardır, sessizce verilmedi):
- **(a) öneri:** `identity_sweep_events()` **aynı imzayla** yeniden tanımlanır ve ayrıca harcanmış, iptal
  edilmiş ya da süresi dolmuş `'change'`/`'revert'` satırlarını **1 gün** sonra siler (definer'a bu tabloda
  `DELETE`). Yeni bir süpürme fonksiyonu §6.1'in *"iki sabit periyotlu süpürme"* sayısını değiştirirdi;
  genişletme değiştirmez. **Ama küçük değildir** — değiştirdikleri, adıyla:
  - ADR 0031 §5.1'in "silme yok" kuralının adlandırılmış istisnaları (`0031:147`, `:154`) ve §5.2'nin
    *"süresi dolmuş taleplerin ve 90 günü geçmiş olayların tek silme yolu"* satırı (`0031:332`) bir tabloyla
    genişler;
  - kemer testi üç yerde: `identityPersonTableDeleters` (`identitybelt_test.go:284-289`) bugün süpürmeye yalnız
    `admin_identity_events`'i tanır, `admin_identity_verifications → identity_sweep_events` girdisi eklenir;
    "the named sweep over another table" kontrolü (`:1626-1627`) yeni tabloyu değil yine başka bir tabloyu
    (`admin_identity_resets`) kullanmalı ki kırmızı kalsın; `identityBeltAllowedCounts`'ta süpürmenin silme
    sayısı 1 → 2.
- **(b)** satırları silmeyen bir temizlik: harcanınca/iptal edilince/süresi dolunca `address = NULL` (CHECK
  "adres yalnız canlı change/revert satırında" olur, `UPDATE (address)` hücresi) — süresi dolmuş satırlar için
  yine periyodik bir yazar gerekir, yani (a)'nın kemer değişikliklerinin çoğu kalır.
- **(c)** süpürme yok: adresler süresiz kalır.
- **Süre:** 1 gün (öneri) · 30 gün · 90 gün (olaylarla aynı).

**Ön koşul (her seçenekte):** süpürmenin bugün Go'da çağıranı yok (§2 son satır, T294) — EC-2 onu
`cmd/tappa/main.go`'ya taslak süpürmesinin yanına bağlar.

### 5.10 Bayrak ve yüzey

Yeni bir bayrak **yok**. Rotalar `mountGated` ile `SurfaceAccountIdentity`'nin altında bağlanır
(`internal/adminauth/flag.go:42`; bayrak kapalıyken panelin hiç sahip olmadığı yolun 404'ü, ADR 0031:623-627);
bayrak bugün `"on"` (`deploy/k8s/05-config.yaml:220`) ve contract A'da kalkar. Bağlanmamış satırın oturumu
formu görmez (§3). İstek ve onay rotaları panelin oturum isteyen yazma grubunda; geri alma rotası **oturumsuz**
alt grupta (§5.3'ün son paragrafı).

## 6. Geçiş — expand/contract ve geri dönüş

- **Tek deploy:** 00101 + 00102 + 00103 + yeni binary. Migration Job rollout'tan önce koşar; eski pod yeni
  şemada çalışır, çünkü 00101 yalnız **ekler/genişletir**: yeni sütunlar `NULL`, `purpose` ve olay CHECK'leri
  genişler, eski binary'nin yazdığı `'verify'`/`'link'` satırları (adres `NULL`, 24 saat) yeni CHECK'lerden
  geçer; `identity_verify_email` ve `identity_link_by_email` değişmez. 00102 yalnız **yeni** fonksiyon yaratır.
  00103 `identity_sweep_events`'i aynı imzayla değiştirir; eski binary onu çağırmaz.
- **Geri dönüş = önceki imaj** (şema geride kalır, eski binary ona uyar). Down'lar ancak **sonraki** bir
  deploy'da, yeni fonksiyonları çağıran binary gittikten sonra; Down hiçbir şeyi önceki binary'nin çağırdığı
  şekilde düşürmez. 00101'in Down'ı `'change'`/`'revert'` satırları varken CHECK'i geri koyamaz: Down canlı
  (süresi dolmamış, harcanmamış) bir geri alma linki varken **ve** `admin_identity_events`'te `email_*` olayı
  varken **sabit mesajla durur** (§5.2; 00079'un Down'ı, `00079:471-476`) — geri alma hakkını sessizce silmez
  ve R17'nin silinemez olay satırlarına dokunmaz; olaylar 90 günlük süpürmeyle yaşlanınca (T294 bağlandıktan
  sonra) Down mümkün olur. Süresi geçmiş linkleri siler. 00102'nin Down'ı `identity_open_reset`'i 00072'nin
  tanımına ve ACL'ine bayt bayt geri koyar. Değiştirilmiş adresler veri olarak kalır.
- **00100'e bağımlılık:** 00100 (`m14-tagshape` şeridinin) bu deploy'dan önce ya da onunla aynı deploy'da
  gitmelidir — EC-1'in Down gidiş-dönüş ölçüsü (00100 ↔ 00101) ve goose'un sırası buna dayanır. 00100 başka bir
  dalda kalırsa numaralar orkestratörle yeniden konuşulur.
- **Contract B ile ilişki (ADR 0031:1434-1450; `docs/plan/m14-tek-kimlik.md:966-973`):**
  `identity_revert_email_change` bağlı satırlara digest (nöbetçi) **kopyalar**, dolayısıyla M14-20b'nin 1b
  listesine girer (contract B'den sonra satıra `NULL`). `identity_change_email` digest'e dokunmaz; contract
  B'nin yeni tetikleyici kuralı (*"`email` yalnız `tappa_identitydefiner`"*) onunla uyumludur. EC-3 M14-20b
  kartına bu satırı ekler.
- **Contract A ile ilişki:** K-E9 (a)'nın "eski adreste bağsız satır" denetimi definer'ın içindedir
  (kimliğin kendi adresiyle anahtarlı adlı e-posta-geniş okuma, `identityEmailWideReads` — tenant'ı önceden
  bilinmez, açık `tenant_id` taşımaz); Go tarafındaki ön kontrol M14-22'nin
  `HasBusinessToAdd`'ını kullanırsa M14-20a'nın o okumayı taşıma işine bağlanır (ADR 0031:595-597).

## 7. CLAUDE.md §4 ile ilişki

- **§4.5 (3):** kapalı listeye üç fonksiyon ve matrise `UPDATE (email)` — *"listeye fonksiyon eklemek … sütun
  matrisini … genişletmek kullanıcı onayı + ADR 0031 değişikliği ister"*. Bu ADR o onayı **ister** (K-E1);
  onaydan sonra EC-1 ADR 0031 §5.1 ve §5.2'ye tarihli bir not ekler (kapalı liste, matris, `identity_open_reset`'in
  yeni imzası, kemerin dört eki, K-E1 (iii) (a) ise "silme yok" istisnasının genişlemesi). Yeni adlandırılmış
  istisna yok (§5.3); geri alma linki K-E1 (ii) seçilirse §6.1 ve CLAUDE.md'de adıyla sayılır.
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
6. **(Yalnız K-E9 (d) seçilmezse) önceki gönüllü bir değişimden sonra gelen saldırıda yeni adresin sahibinin
   geri dönüşü yoktur.** Kişi A0 → A yapar (A0'a geri alma linki); 7 gün içinde bir saldırgan A → B yapar.
   "Tek canlı geri alma linki, en eskisi" kuralı yüzünden yeni link yazılmaz; A'nın sahibi (kişi) geri alamaz,
   yalnız A0'ın sahibi (ör. eski işveren) alabilir. (d) bunu kapatır.
7. **Saldırganın pencerede kurduğu üyelikler geri almadan sonra durur** (§5.6, S-2); kişi onları K-E11 (a)'nın
   sıfırlamasının açtığı `reset` penceresinde görür ve çözer.
8. **Geri alma, kişinin bağlı bir işletmesinde eski adresli başka bir satır varken reddedilir** (YÜKSEK-1;
   §5.3). Bugün bunun bilinen tek kaynağı kişinin o işletmede **kendi reddettiği** satırdır (reddedilen satır
   eski adresi taşır, `00072:903-905`) ve kişinin sonra aynı işletmeye yeni adresle davetle dönmesidir. Ret
   0 satırdır, 500 değil; adres hiçbir hataya girmez. Bir saldırganın böyle bir satırı kurbanın eski adresiyle
   doğurup geri almayı bloke edecek ürün yolu bulunamadı (okundu: satır doğuran yollar — kayıt sahiplenmesi,
   davet kabulü — eski posta kutusunun tıklamasını ister); `tappa_app`'ın `admin_users INSERT (email)` yetkisi
   contract B'ye dek durur (ADR 0031 §8 adım 8.4) ve onu kullanan bir uygulama yolu açılırsa bu risk büyür.
9. **Rollout dakikalarında eski binary `identity_open_reset`'i alıcı denetimi olmadan çağırır** (S-1, §5.3) —
   bugünkü davranış; yeni binary devraldığında kapanır.

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
| **K-E1** | Kapalı liste, matris ve kemer eki | **(i) kapsam:** (a) §5.2–§5.3: üç yeni fonksiyon (`identity_request_email_change`, `identity_change_email`, `identity_revert_email_change`); `identity_open_reset`'in dördüncü argümanı (S-1, imza değişir); `admin_identity_verifications`'a iki amaç + `address` + `cancelled_at`; definer'a `UPDATE (email)` iki tabloda, doğrulama tablosunda `SELECT/INSERT/UPDATE` hücreleri; üç olay; **kemerin dört eki** — istekte adlı e-posta-geniş okuma (`identityEmailWideReads`), değişim ve geri almada adresle anahtarlı kimlik okuması (dördüncü sınıf, `identityClaimAddressKeys` emsali), süpürmenin ikinci tablosu (`identityPersonTableDeleters`, "another table" kontrolü, `identityBeltAllowedCounts` 1 → 2) · (b) ayrı bir tenant-kapsamsız tablo (CLAUDE.md §4.5 (3) + §6 metni, R5, restore sayıları değişir) · (c) hiç yapma. **(ii) geri alma linkinin §6.1'deki adı:** (a) adıyla sayılır — *"e-posta değişikliğinin geri alma linki"*; §6.1 + CLAUDE.md §4.5 (3) bayt-aynı değişir · (b) *"doğrulama token'ı"* sözcüğüne sığdırılır, metin değişmez. **(iii) saklama (§5.9):** (a) süpürme genişler, 1 gün · (b) adres NULL'lanır · (c) süpürme yok; süre 1 / 30 / 90 gün | **(i) (a) · (ii) (a)** — tek sırla adres + parola yazan bir link sıfırlama sınıfındadır, adıyla görünmeli · **(iii) (a), 1 gün** |
| **K-E2** | "Bu ben değilim" yolu | (a) eski adrese bildirim + tek kullanımlık geri alma linki · (b) yalnız bildirim (geri dönüş yok; operatör de yardım edemez) · (c) bekleme süreli değişim: değişim N gün sonra yürürlüğe girer, eski adreste iptal linki | **(a)** — ana akımın karşılığı: Google/Microsoft eski adrese "bu sen değilsen hesabını güvene al" bağlantısı gönderir |
| **K-E3** | Geri alma linkinin ömrü | (a) 24 saat (tablonun bugünkü tavanı) · (b) 72 saat · (c) 7 gün | **(c) 7 gün** — hafta sonu okunmayan bir kutu; bedeli §8 risk 1 |
| **K-E4** | Değişim linkinin ömrü | (a) 24 saat · (b) 1 saat | **(a)** — doğrulama linkiyle aynı; iki sır ister |
| **K-E5** | Çakışmada yeni adrese ne gider | (a) linksiz bilgi mektubu (*"Someone asked to use this address for a Taptime sign-in. It already has one, so nothing changed."*) · (b) hiçbir şey | **(a)** — iki kol aynı mektup sayısını öder; adresin gerçek sahibine haber verir |
| **K-E6** | Değişimden sonra oturumlar | (a) onaylayan oturum kalır, öbürleri her işletmede kapanır · (b) hepsi kapanır (yeniden giriş) · (c) hiçbiri | **(a)** — parola değişiminin emsali; geri alma her durumda hepsini kapatır |
| **K-E7** | Eski adrese istek anında da haber | (a) yalnız değişim anında · (b) istek anında da | **(a)** — istek tek başına hiçbir şeyi değiştirmez |
| **K-E8** | Eski adres bildirimi yeni adresi anar mı | (a) anmaz · (b) maskeli anar · (c) tam anar | **(a)** — eski kutunun sahibi artık başkası olabilir (§8 risk 1); kişinin yeni adresi ona söylenmez |
| **K-E9** | İsteğin ön koşulları | (a) mevcut adres doğrulanmış **ve** onay penceresi kapalı **ve** eski adreste bağsız aktif işletme yok · (b) yalnız doğrulanmış adres · (c) hiçbiri · **(d) ek olarak:** kimliğin canlı bir geri alma linki varken yeni değişim isteği reddedilir (geri alma penceresi başına tek değişim; "en eski link" zincir kuralı kalkar) | **(a) + (d)** — doğrulanmamış adres değişirse `first_verify` penceresi hiç açılmaz (ADR 0031 §5.8) ve bildirim kanıtlanmamış bir kutuya gider; pencere açıkken Undo kırılır; bağsız satırlar banttan düşer; (d) §8 risk 6'yı kapatır, bedeli 7 gün içinde ikinci bir değişim yapamamak. (d) seçilmezse §8 risk 6 kabul edilmiş olur |
| **K-E10** | Kullanıcıya görünen metinler | **(i) istek yanıtı (flash):** (a) adres anmaz — *"Check your inbox at the new address. The change happens when you open the link there."* (kapalı sözcük listesinden, URL'de değer yok) · (b) adresi anar — dar yollu, kısa ömürlü, `HttpOnly` bir çerezle taşınır (doğrulama çerezinin deseni, `accountidentity.go:68`, `:263-276`), bir kez okunur ve silinir; adresin URL'ye, log'a, `Referer`'a girmediğini pinleyen bir test. **(ii)** form başlığı *"Change your sign-in email"* · **(iii)** onay *"Change your sign-in email to the address this link was sent to?"* · **(iv)** ret *"This link can't be used. Ask for a new one from your account page."* · **(v)** ön koşullar *"Confirm your current email first."*, *"Finish confirming your businesses first."*, *"Add your other businesses first."*, *"You changed your email recently. You can change it again after {tarih}."* (K-E9 (d)), *"That's already your address."* · **(vi)** eski adres mektubu, linkli: *"Your Taptime sign-in email was changed on {tarih}. If this wasn't you, press the button below within 7 days to undo it."*; linksiz (zincir, K-E9 (d) seçilmezse): *"Your Taptime sign-in email was changed on {tarih}. If this wasn't you, this address can no longer undo it; the earlier change's undo link still works."* · **(vii)** K-E14 (b) ise yeni adrese: *"This address is now your Taptime sign-in email."* · **(viii)** geri alma sonucu *"Your sign-in email is back. We sent a link to set a new password."* | **(i) (a)** — en küçük kanal; kişi adresi az önce kendisi yazdı. Geri kalanı kullanıcı onaylar ya da düzeltir |
| **K-E11** | Geri almadan sonra | (a) eski adrese otomatik bir sıfırlama linki (`p_recipient` bağıyla, S-1) · (b) kişi "Forgot password"u kendisi kullanır | **(a)** — bir adım eksik, aynı güvenlik (link yine eski posta kutusuna) |
| **K-E12** | Ne zaman | (a) şimdi, contract'tan önce, §5.5'in kurallarıyla · (b) contract B'den sonra (eski kol yok, satır başı link yok) | **(a)** — contract A en erken 2026-12-27 ve tek bağsız owner kaldıkça ertelenir (`state.md:807`); ek kurallar küçük |
| **K-E13** | Yalnız harf büyüklüğü değişikliği | (a) aynı adres sayılır, reddedilir · (b) izin verilir (yazım düzeltmesi) | **(a)** — citext'te eşit; yazım isteği nadir |
| **K-E14** | Değişimden sonra **yeni** adrese onay mektubu | (a) yok · (b) var — linksiz, bildirimle aynı yoldan (`SendExempt`, kimlik başına tavanı paylaşır), metin K-E10 (vii) | **(a)** — yeni kutunun sahibi linki az önce açtı ve sayfanın cevabını gördü; bir mektup daha bilgi eklemez |
| **K-E15** | Askıdaki bir işletme geri almayı engeller mi | (a) engellemez — geri alma bir güvenlik eylemidir, askıdaki işletmeye de `admin.email.reverted` yazılır · (b) askıdaki işletmede aynaya dokunulmaz (kişinin adresi işletmeler arasında ayrışır — `identityOfSession` kırılır, `identityaccount.go:139-151`) | **(a)** — (b) aynayı bozar; EC-2 ADR 0025'in istisna listesine karşı doğrular |

## 11. Testler (özü — adlar yapıcının; henüz var olmayan test burada adlandırılmaz)

- **Yetki ve katalog:** imza tablosu, fiil × sütun matrisi ve "her yetki kullanılıyor" egzersizi üç fonksiyonu ve
  yeni hücreleri kapsar (`TestIdentityCatalog_TheFunctionsAndTheirExactSignatures`,
  `TestIdentityDefiner_ColumnMatrix`, `TestIdentityDefiner_EveryGrantIsUsed`); `tappa_app`, `tappa_operator`,
  `tappa_opdefiner` hâlâ `admin_users.email`'e yazamaz ve kimlik tablolarında yetkisiz
  (`TestIdentityFunctions_NoOperatorRoleMayExecuteThem`); `privCatalogAllowed` güncellenir.
- **Kemer:** üç gövde kimlik kemerinde, aile başına sayıyla (`identityBeltFamilies` deseni); her tenant-tablo
  ifadesi açık `tenant_id` — **istisna** istekteki eski adres okuması, adlı e-posta-geniş okumadır
  (`identityEmailWideReads`); değişim ve geri almadaki "adreste kimlik yok" okuması dördüncü sınıftır
  (`identityClaimAddressKeys`'in emsali, sayıyla); K-E1 (iii) (a) ise süpürmenin ikinci tablosu
  (`identityPersonTableDeleters`, "another table" kontrolü, `identityBeltAllowedCounts`); adres kuralı her yazarda
  (`TestIdentityAddressRule_EveryIdentityInsertDecidesIt`'in kapsamı `UPDATE … SET email`'e genişler).
- **Davranış (gerçek Postgres):** 1, 2 ve 10 işletmeli kimlikte değişimden sonra ayna = kimlik adresi her
  satırda; tetikleyici `tappa_app`'ın ve `tappa_owner`'ın doğrudan yazmasını hâlâ reddeder; her ret dalı 0 satır
  ve hiçbir yazı; eşzamanlı iki onay → tam bir başarı, 500 yok (`-race`, N goroutine); aynı adrese eşzamanlı
  değişim + kayıt sahiplenmesi + davet kabulü → biri kazanır, 500 yok, `DETAIL`'de adres yok; K-E9 (d) ise
  canlı geri alma linkiyle istek 0 satır, değilse zincir (A→B→C, iki geri alma) → yalnız en eski link geri alır;
  geri alma, reddedilmiş eski-adresli satırı olan bir işletmede 0 satır, 500 yok (YÜKSEK-1); geri alma ↔ geçiş /
  giriş / bağlama eşzamanlı → sonunda canlı oturum 0 (S-2); sıfırlama açılışı ↔ değişim eşzamanlı → link ya
  iptal edilmiş ya hiç basılmamış, eski adrese canlı link yok, kilitlenme yok (S-1).
- **Düz zamanlama (HTTP, gerçek Postgres):** çakışmasız / kimliğe çakışan / kendi adresi olmayan üç kol iç içe;
  her yanıt tabanı öder, medyanlar aynı bantta (`TestPanelIdentityReset_TimingIsFlatOverHTTP`'nin biçimi).
- **Log/audit sızıntısı:** röle davranışlarının hepsinde log anahtarları ADR 0022 §10 + `identity_id`; hiçbir
  satırda adres (her harf büyüklüğünde), token, hash; audit satırında yalnız `outcome` ve sıfır değilse sayı
  (`TestPasswordNotice_LogsOnlyIdsClassAndCode`'un biçimi).
- **Mutasyon kanıtları:** her yeni koruma için (yüklemi/predikatı kaldır → kırmızı → geri al) — liste kartta.
