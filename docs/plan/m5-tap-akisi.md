# M5 — Tap akışı (uçtan uca)

**Amaç.** Çalışanın gördüğü ürünü tamamlamak: davet → aktivasyon → plakete
dokunuş → tek buton → onay. Uygulama kurulumu yok, öğrenme yok.

**Bittiğinde:** Gerçek bir telefonla (veya simüle edilmiş SUN URL'siyle) uçtan
uca check-in/out çalışıyor, kayıtlar veritabanında, kararlar §5'e uygun.

**Ana araçlar:** skill `tappa-brand` (her ekran işinde) · skill `tappa-sun` ·
agent `tappa-security-auditor` (milestone sonunda)

> **Tap ekranı kutsaldır** (CLAUDE.md §9): tek ekran, tek buton, sıfır öğrenme.
> Onay ekranında buton **yok**. Bu ekrana özellik eklemek istiyorsan **önce sor**.

---

## M5-01 — `internal/session`: oturum yaşam döngüsü

- **Bağımlılık:** M1-04 · Q11
- **Kırmızı çizgi:** §4.1 (biyometri yok) · §4.7 (token loglanmaz)
- **Commit:** `feat(session): issue, verify and revoke session tokens`

**Amaç.** *Proof of person*: telefonun sahibini tanıyan kalıcı oturum.

**Tasarım.**
- Token kriptografik rastgele (≥32 byte), **yalnızca çereze** yazılır.
- DB'de `token_hash` saklanır (HMAC-SHA256, `TAPPA_SESSION_HMAC_KEY`).
- Çerez: `httpOnly` + `Secure` + `SameSite=Lax` + uzun `Max-Age` (~1 yıl),
  kullanımda yenilenir (`last_used_at`).
- İptal anlık: `revoked_at` damgası → sonraki istek düşer.
- Cihaz limiti (1–2) opsiyonel, config ile kapalı gelebilir.

**Kabul kriterleri.**
- Ham token DB'de, log'da, hata mesajında **hiç** yok — testle kanıtlanır.
- Doğrulama **tek sorgu** (`GetEmployeeBySessionHash`); zamanlama yüzeyi
  gerekçelendirilmiş ve Go tarafında sır karşılaştırması yok (aşağıdaki
  düzeltme bloğu).
- Deaktive çalışanın oturumu **anında geçersiz kılınabilir**: iptal yüzeyi
  (`RevokeSession` / `RevokeSessionsForEmployee`) var, iptal edilmiş oturum
  sonraki doğrulamada **düşer** — ama *deaktiflik kararı* session katmanında
  DEĞİL, `sys:employee-deactivated` guardrail'inde kalır (§5 satır 4: kayıt
  yazılır). Gerekçe aşağıdaki düzeltme bloğunda.
- **Q11 — kodla karşılanmaz, AÇIK kalır.** Sunucu tarafı (uzun `Max-Age`,
  `httpOnly`, `Secure`, `SameSite=Lax`) M5-01'de yazıldı ve doğrulandı; gerçek
  bir iPhone'da NFC → Safari akışıyla çerez ömrünün ölçülmesi **kullanıcının
  eylemidir** ve M5-01'i bloklamaz. Ölçüm yapılınca sonucu
  `open-questions.md`'ye yazılır. Ölçüm kod tasarımını değiştirmez; değiştirirse
  ayrı bir görev açılır.

> **Kart düzeltmesi (2026-07-31, M5-01 uygulaması sırasında).** Üç kriter
> gerçekle çelişiyordu; üçü de **zayıflatılmadan** yeniden yazıldı.
>
> **1) "Tek sorgu" ile "deaktive oturum anında geçersiz" aynı anda tutulamıyor —
> ve tutulmaya çalışılırsa §5 İHLAL EDİLİR.** Ölçüm:
> [`00003`](../../db/migrations/00003_create_employees_sessions.sql) satır
> 189-205'teki `resolve_session_by_token_hash` yalnızca
> `(id, tenant_id, employee_id, revoked_at)` döndürür — **`employees.status`
> yok**. Yani o tek sorgu deaktifliği göremez; görebilmesi için ya ikinci bir
> sorgu ya uygulanmış migration'ın değiştirilmesi gerekirdi (§6: yasak).
>
> Asıl mesele performans değil, **doğruluk**: session katmanı deaktif bir
> oturumu sessizce "oturum yok"a çevirirse, §5 **satır 3** (aktivasyon sayfası,
> **kayıt YAZILMAZ**) devreye girer ve deaktive çalışanın denemesi **iz
> bırakmadan kaybolur**. Oysa §5 **satır 4** o denemeyi `reject` + log +
> güvenlik uyarısı olarak **kayda geçirmeyi** emreder (§4.6 "kayıt asla
> kaybolmaz"). Karar-yeri hatası bir kırmızı çizgi ihlaline dönüşürdü.
>
> **Doğru sınır: session katmanı gerçeği taşır, karar vermez** — 00003'ün
> `revoked_at` yorumundaki ("iptal kararı domain katmanındadır") ilkeyle birebir
> aynı. Deaktiflik otoritesi zaten `internal/policy`'nin `sys:employee-deactivated`
> guardrail'i + `tap.Decide`'dır (M4'te bitti; `tap.Input.Employee` nil ile
> `deactivated`'ı bilerek ayırır).
>
> **Düşürülen garanti neyle karşılanıyor** (M0-02 2. tur dersi — zayıflatma
> değil, yer değiştirme):
> 1. Session katmanı `revoked_at`'ı **doğrular**: iptal edilmiş oturum `Verify`
>    çağrısında `ErrRevoked` alır, `Touch` fail-closed düşer.
> 2. İptal yüzeyi **var ve testli**: `Revoke`, `RevokeSessionsForEmployee`
>    (bir çalışanın tüm canlı oturumları, idempotent). Meşru çağıranlar:
>    **çalınan/kaybolan telefon** ve **ikinci-aktivasyon kararı (M5-02)**.
>    ⚠️ **Deaktivasyon (M6-05) bunu ÇAĞIRMAZ** — gerekçe aşağıdaki 3. tur ekinde;
>    bu satır önce "M6-05 çağırır" diyordu ve 3. turda çürütüldü.
> 3. Tap yolunda otorite **guardrail**'dir: `employees.status` okunur, kayıt
>    yazılır, uyarı üretilir. Bu yüzden `Verify` iptal durumunda bile
>    **çözümlenen gerçekleri döndürür** (`Resolved` dolu + `ErrRevoked`) —
>    çağıran kaydı yazabilsin diye. Kayıt kaybı yolu yapısal olarak kapalı.
>
> **2) "sabit zamanlı" kriteri, olmayan bir karşılaştırmayı istiyordu.** Ana
> arama Postgres'te `sessions.token_hash` UNIQUE indeksi üzerinden yapılır ve bu
> **sabit zamanlı değildir**. Kabul edilebilir olmasının gerekçesi yazıldı
> (`internal/session/manager.go`, `Verify` doküman bloğu): aranan değer, sunucu
> tarafındaki anahtarla üretilmiş bir **HMAC-SHA256 çıktısıdır**. Zamanlama
> sinyalini gerçek bir satıra doğru yürütmek için saldırganın *hash*'i seçmesi
> gerekir; hash'i seçmek HMAC'i hesaplamayı, o da anahtarı bilmeyi gerektirir.
> Çerez değerini serbestçe seçebilir, karşılık gelen hash'i seçemez. Ayrıca
> komşu hash'ler sızsa bile işe yaramaz: çerez bir **ön-görüntü** taşımak
> zorundadır. Go tarafında bu pakette **hiçbir sır karşılaştırması yoktur**
> (eşleştirmeyi DB yapar); eklenecek olursa `crypto/subtle.ConstantTimeCompare`
> zorunludur (redline R7) — `==` veya `bytes.Equal` değil.
>
> **3) "Q11 ölçüldü" bir kabul kriteri olamaz.** Kriter, M5-01'in ancak gerçek
> bir iPhone'la kapanabileceğini söylüyordu; ölçüm **kullanıcının eylemidir**,
> yapıcı ajanın erişemeyeceği bir donanım ister ve kod tasarımını
> değiştirmez — sunucu tarafı zaten `Set-Cookie` + `httpOnly` + `Secure` +
> `SameSite=Lax` + ~1 yıl `Max-Age` yazıp test eder. Kriter, M5-01'in gerçekten
> teslim ettiğini söyleyecek ve Q11'i **açık** bırakacak biçimde yeniden yazıldı.
> `open-questions.md`'ye **dokunulmadı** (orkestratörün).
>
> **Dokunulmayan:** Cihaz limiti kart gereği opsiyonel bırakıldı — M5-01 yalnızca
> sorgu yüzeyini (`ListForEmployee`, `RevokeSessionsForEmployee`) verir, politika
> uygulamaz.
>
> **3. tur eki (aynı gün, `tappa-security-auditor` RED sonrası).** Denetim,
> 2. turdakiyle **aynı sınıftan** bir hata buldu: dosya, ölçmediği bir garantiyi
> yorum olarak beyan ediyordu. `cookie.go` "hiçbir çağıran prod'da Secure'suz
> çerez üretemez" diyordu; **yanlıştı.** `Cookies` alanı `secure bool` olduğu için
> **sıfır değer = Secure'suz** demekti ve Go, başka bir paketin unexported alanı
> *adlandırmasını* yasaklar, sıfır değeri **elde etmesini** değil:
> `var c session.Cookies`, `session.Cookies{}` ve — asıl tehlikelisi — bir handler
> struct'ında **alanı hiç yazmamak** (Go'da derleme hatası değildir). Üçü de
> ölçüldü, üçü de `HttpOnly; SameSite=Lax` yazıp **`Secure` yazmıyordu**.
>
> **Düzeltme — alanın kutbu çevrildi:** `insecure bool`, `Secure() = !insecure`.
> Artık **sıfır değer fail-closed (Secure)** ve gevşemeyi yalnız `NewCookies`
> isteyebilir (tek dal: prod DEĞİL **ve** BaseURL https DEĞİL). "Set/Clear
> kurulmamış codec'i reddetsin" alternatifi yerine kutup çevirme seçildi: sentinel
> alan, hata döndürmeyen bir metotta hata yolu ve disiplin gerektirmiyor —
> tehlikeli durum **kazara temsil edilemez** hâle geliyor. Regresyon testi harici
> test paketinden yazıldı (üç sıfır-değer yolu + dev/http'nin hâlâ gevşediğini
> gösteren pozitif kontrol).
>
> **Bu turda düzeltilen üç şey daha:** (1) `Token` karşılaştırması artık **kimlik**,
> değer değil (`*string` sonrası aynı değeri saran iki Token `!=`) — yorum gerçeğe
> indirildi ve teste bağlandı; sonuç fail-closed, açık değil. (2) **Deaktivasyon
> oturum iptal ETMEMELİ**: `sys:employee-deactivated` yalnız `employees.status`'e
> bakar, iptal reddetmeye hiçbir şey katmaz ama her sonraki tap'i `ErrRevoked`
> dalına iterek §4.6 kayıp koşulunu üretir. `RevokeAllForEmployee`'nin meşru
> çağıranları çalınan/kaybolan telefon ve M5-02 ikinci-aktivasyon;
> belge tersine çevrildi (kod değişmedi). (3) `IssueParams.DeviceInfo` **sınırlandı**
> (≤64 rune, geçerli UTF-8, Unicode `Cc` **ve** `Cf` reddedilir — U+0085 NEL ve
> U+202E RTL-override dahil —, **kırpma değil RED** — §7 sessiz kabul yasağı;
> CSV formül enjeksiyonu ve HTML kaçışı bilinçli olarak **bu katmanın işi değil**,
> sırasıyla M6-07 CSV yazıcısı ve templ şablonu);
> gerçek UA'lar reddediliyor, meşru kaba etiketler dokunulmadan geçiyor.
>
> **Kapsam genişlemesi (agent-brief madde 8):** `TAPPA_ENV` artık **kapalı kümede**
> doğrulanıyor (`internal/config`, M5-01 kapsamı dışı). Gerekçe: bu değer
> `IsProd()` üzerinden çerez sertleşmesini seçtiği için artık bir güvenlik
> nitelidir; `TAPPA_ENV=production` yazımı `IsProd()`'u sessizce false yapardı.
> Repodaki tüm mevcut değerler tarandı (`.env`, `.env.example` = `dev`; CI hiç
> set etmiyor → varsayılan `dev`), hiçbiri kırılmadı.
>
> **2. tur eki (aynı gün, üçüncü göz RED sonrası).** Bağımsız denetim, bu kartın
> "ham token hiçbir yerde yok" kriterinin ilk uygulamada **karşılanmadığını**
> ölçtü: `session.Token`'ı **unexported** bir alanda taşıyan sıradan bir
> handler struct'ı `%v`/`%+v`/`%#v`/`slog` ile ham değeri basıyordu (`fmt`,
> `CanInterface()==false` olduğu için `Formatter`/`Stringer`'ı atlar). Düzeltme:
> değer **dolaylı** tutuluyor (`*string`) → aynı yol artık adres basıyor.
> Regresyon testi **harici bir test paketinden** (`package session_test`)
> yazıldı, çünkü hata tam da paket sınırının dışında ortaya çıkıyor. Kalan sınır
> açıkça yazıldı: kasıtlı `reflect` okuması hâlâ mümkün ve bu "imkânsız" diye
> belgelenmiyor.

**Tuzaklar.**
- `SameSite=Strict` NFC → tarayıcı açılışında çerezi göndermeyebilir; `Lax` seç
  ve gerçek cihazda doğrula.
- Telefonun kendi ekran kilidi **bizim katmanımız değil** — ondan veri alma,
  WebAuthn/attestation ekleme (§4.1).

---

## M5-02 — Davet ve aktivasyon akışı

- **Bağımlılık:** M5-01 · Q02
- **Kırmızı çizgi:** §4.7 (davet kodu loglanmaz)
- **Commit:** `feat(activation): add invite and one-time activation flow`

**Amaç.** Çalışanın ilk kez tanınması: davet linki → tek kullanımlık kod →
oturum çerezi.

**Kabul kriterleri.**
- Davet kodu tek kullanımlık, süreli, kullanıldıktan sonra ölü.
- Aktivasyon `POST /api/activate` → `Set-Cookie` (httpOnly) → çalışan `active`.
- Kod tahmin edilemez ve **loglanmaz**.
- **Kod entropisi ≥128 bit — DEĞİLSE katı kilitleme ZORUNLU.** Şema (00009) hiçbir
  kilitleme durumu taşımaz ve bu yalnızca ≥128-bit kodla savunulabilir. Kod bundan
  kısaysa (insan okuyan/yazan her biçim dahil) şunlar **birlikte** gerekir: davet
  ve kaynak IP bazlı **deneme sayacı + kilit damgası** (yeni migration), **kısa
  TTL**, başarısız her denemenin **`audit_log`'a** yazılması, `/api/activate`'e
  **kendi dar oran sınırı** (M5-03'ün geniş tap sınırı kapsamaz).
  ⚠️ **Bunu yakalayacak mekanik bir kontrol YOKTUR:** `code_hash` CHECK'i hash'in
  **şeklini** sınar, kodun entropisini değil — 6 haneli bir kodun hash'i de
  64-hex'tir (ölçüldü). Yani bu kriter yalnızca **okunarak** tutulur; kısa koda
  geçen değişiklik bu maddeyi karşıladığını raporunda **göstermek zorundadır**.
- **Aktivasyon ⇒ tüketim.** Bir çalışanı `active` yapan tek veri-katmanı yolu
  `ConsumeInviteAndActivate`'tir (tek ifade: davet tüketilmeden aktivasyon olmaz).
  B fazı ayrı bir "aktive et" sorgusu **eklemez**; gerekiyorsa (M6 müdür eylemi)
  ayrı bir görevde, ayrı gerekçeyle gelir.
- Zaten aktif çalışanın ikinci aktivasyonu: yeni cihaz mı, saldırı mı — davranış
  belirlenmiş ve testli.
- §5 satır 3'ün **hedefi** buraya düşer: aktivasyon sayfası var, erişilebilir ve
  hiçbir **`transactions`** satırı yazmıyor (§5 satır 3'ün yasakladığı budur).
  ⚠️ "Bir kod harcanana kadar hiçbir kayıt yazmıyor" **değil**: tüketilmiş bir
  kodla gelen `GET /activate` bir `audit_log` satırı yazar ve bu **doğrudur**
  (§4.6 reddedilen denemenin iz bırakmasını ister) — yanlış olan eski cümleydi,
  davranış değil. ⚠️ **Yönlendirmenin kendisi
  M5-02'nin işi DEĞİL:** "oturum yoksa tap → aktivasyon sayfası" dalını kuran uç
  nokta `GET /t`'dir ve o **M5-04**'tür. Bu satır önce "buraya bağlanır" diyordu;
  M5-02 bittiğinde bağlanamayacağı ölçüldü (tap uç noktası yok) ve kriter gerçeğe
  indirildi — düşen bir garanti yok, yalnızca sahibi doğru göreve yazıldı.
- **WiFi adımı (Q14):** aktivasyon akışında çalışandan mekânın WiFi'ına bağlanması
  istenir (ağ adı lokasyon kaydından gösterilir), "neden" tek cümleyle açıklanır.
  Atlanabilir — zorunlu tutulmaz — ama atlandığında sonraki tap'ler GPS yoluna
  düşer. IP kanıtının (50 puan) pratikte var olması buna bağlı.

- **GDPR Art. 13 aydınlatma metni burada gösterilir** — çalışanın veri işlemeyi
  ilk gördüğü yer. İçerik: tap anında GPS ve kaynak IP okunduğu, mesai kaydının
  ne kadar saklandığı (Q13), veri sorumlusunun işveren olduğu, biyometrik veri
  **toplanmadığı**. Aktivasyon bu ekran onaylanmadan tamamlanmaz.

**Tuzaklar.**
- Davet e-postası Q02'ye bağlı. Q02 cevapsızsa akışı kodla ama gönderimi arayüz
  ardına al (kodu panelde göster) — görevi tamamen bloklama.
- **Ama "kodu panelde göster" kalıcı çözüm DEĞİL** (Y-D). Davet kodunu üreten ve
  gören kişi ile bordroyu şişirmekte en güçlü teşviki olan kişi aynı: müdür sahte
  bir profil açar, kodu ekrandan okur, kendi telefonunun ikinci tarayıcı
  profilinde aktive eder → her gün trust 100, gerçek çalışandan ayırt edilemez.
  Q02 çözülünce kod **çalışanın kendi kanalına** (e-posta/SMS) gider; bu
  zorunludur, opsiyon değil. O zamana kadar risk ADR 0005'te kayıtlı ve tespit
  sinyali M6-11'de.
- Kod entropisi ve deneme sınırı belirtilmeli: ya ≥128 bit, ya da insan okuyacak
  kısa kod ise **katı kilitleme** (IP+kod bazlı, kısa TTL, başarısız deneme
  `audit_log`'a). M5-03'ün tap oran sınırı bilinçli olarak geniş — `/api/activate`
  onun kapsamına **girmez**, kendi dar sınırı olur. 10⁶ uzay + sınırsız deneme +
  15 bekleyen davet = saniyeler içinde ele geçirilen bir kimlik.

> **Kart düzeltmesi (2026-07-31, M5-02 A fazı sırasında).** Görev **iki faza**
> bölündü (orkestratör kararı) ve kart bunu söylemiyordu; bölünme kriterleri
> **değiştirmez**, yalnızca hangi fazın hangisini kapattığını sabitler.
>
> **A fazı (bitti) — yalnız veri katmanı.** Migration
> [00009](../../db/migrations/00009_create_employee_invites.sql)
> (`employee_invites` + RLS beşlisi + `REVOKE DELETE` + çözümleme fonksiyonu),
> [`db/queries/invites.sql`](../../db/queries/invites.sql) (`CreateInvite`,
> `ConsumeInviteAndActivate`, `ListPendingInvitesForEmployee` — DELETE sorgusu
> **yok**, consume-by-id sorgusu **yok**, ayrı "aktive et" sorgusu da **yok**),
> `internal/db/resolve.go` → `GetInviteByCodeHash`, testler
> `internal/db/invites_test.go` + `rls_test.go`.
>
> **B fazı (açık) — kartın geri kalanı aynen geçerli:** `POST /api/activate`,
> `Set-Cookie`, aktivasyon sayfası, **GDPR Art. 13 metni**, **WiFi adımı (Q14)**,
> §5 satır 3 bağlantısı, uç nokta oran sınırı, kodun üretimi/HMAC'i ve Q02 kanalı.
> Hiçbiri A fazında yapılmadı ve hiçbiri kriterlerden düşürülmedi.
>
> **A fazında bağlanan beş karar** (B fazını bağlar):
>
> 1. **Entropi dalı seçildi: ≥128 bit — ama bunu ŞEMA ZORLAMIYOR, kabul kriteri
>    zorluyor.** Tablo bilerek **hiçbir kilitleme durumu taşımıyor** (deneme sayacı /
>    `locked_until` / IP sütunu yok); bu ancak kriptografik ≥128-bit kod
>    varsayımıyla doğrudur. `code_hash` bir **biçim CHECK'i** aldı
>    (`^[0-9a-f]{64}$` — `internal/session`'ın ürettiği hex HMAC-SHA256 şekli) ve
>    bunun satın aldığı şey **yalnızca şudur: ham, hash'lenmemiş bir kodu sütuna
>    yazma KAZASI** artık veritabanı tarafından reddediliyor (insan-okuyan kod,
>    base64url token, büyük harfli hex, yanlış uzunluk → 23514, ölçüldü).
>    **CHECK entropiyi ÖLÇMEZ** — 6 haneli bir kodun hash'i de tam 64-hex'tir
>    (ölçüldü: `encode(sha256('123456'::bytea),'hex') ~ '^[0-9a-f]{64}$'` → `t`).
>    Yani B fazı kısa bir koda geçip onu düzgünce HMAC'lerse **CHECK yeşil kalır,
>    hiçbir migration gerekmez ve hiçbir mekanik uyarı ateşlenmez.** Bu yüzden
>    yükümlülük yukarıdaki **kabul kriterine** yazıldı; buradaki blok onu
>    hatırlatır, zorlamaz. (CHECK'in kendisi
>    `sessions.token_hash`/`password_resets.token_hash` precedent'inden **bilinçli
>    sapmadır** — gerekçe: iddiayı yazan dosya onu zorlamak zorundadır.) Uç nokta
>    oran sınırı ve başarısız denemenin `audit_log`'a yazılması her iki dalda da
>    **B fazının işi** — şema değil.
> 2. **Tüketim KOD HASH'i ile anahtarlanır (`id` ile DEĞİL) ve aktivasyonla TEK
>    İFADEDE birleşiktir** (iki denetim bulgusu, tek çözüm).
>    `ListPendingInvitesForEmployee` davet `id`'sini tenant içindeki her çağırana
>    verir; id ile tüketen bir sorgu **kodu bilmeyi gereksiz** kılardı. Ayrı bir
>    `ActivateEmployee` sorgusu ise "aktive edildi ⇒ bir davet tüketildi"
>    çıkarımını **çağrı sırasına** bırakıyordu: tenant bağlamındaki bir kod yolu
>    hiç davet harcamadan istediği çalışanı aktive edebiliyordu (ölçüldü).
>    İkisi de **silindi**; yerlerine tek atomik ifade geldi:
>    `ConsumeInviteAndActivate` (data-modifying CTE — tüket, yalnız tüketildiyse
>    aktive et). Bu katmanda `active` olmanın **başka yolu yok**.
>    **Deaktif çalışanda davet YANMIYOR — koruma yapısal.** İlk uygulamada
>    veri-değiştiren CTE koşulsuz çalıştığı için `deactivated` çalışanda damga
>    yazılıyor, aktivasyon olmuyordu; davet yalnızca çağıran hatayı ilettiği için
>    (`WithTenant` rollback) kurtuluyordu — yani koruma **disiplindi**. Ölçüldü:
>    guard'sız ifade COMMIT ile daveti **yakıyor** (`used_at` dolu), guard'lı ifade
>    **yakmıyor** (`used_at` NULL). Düzeltme: iç CTE'nin `WHERE`'ine
>    `AND EXISTS (… employees … status IN ('invited','active'))`. Artık hatayı
>    yutup COMMIT eden dikkatsiz çağıran bile daveti harcayamıyor (test tam da
>    bunu simüle ediyor: hata yutuluyor, transaction commit ediliyor, davet hâlâ
>    tüketilmemiş ve sonradan başarıyla harcanabiliyor).
>    **Çift kontrol bilinçli:** CTE'deki `EXISTS` **damgayı**, dıştaki
>    `status IN (…)` **aktivasyonu** korur; ikincisi bir YARIŞ koruyucusudur
>    (READ COMMITTED yeniden değerlendirmesi), tekrar değil.
>    **Kalan pencere yazıldı (ölçüldü):** deaktivasyon tam da CTE ile dış UPDATE
>    arasında commit ederse damga yazılır ve commit eden çağıran kodu boşa harcar
>    — ama **aktivasyon yine olmaz** (fail-closed); en kötü sonuç yanmış bir kod,
>    yetkisiz aktivasyon değil. Pencereyi kapatmak için `EXISTS (… FOR SHARE)`
>    denendi ve **ölçülerek reddedildi**: aynı çalışanın iki eşzamanlı
>    aktivasyonunu kilit yükseltmesinde **deadlock**'a sokuyor
>    (`ERROR: deadlock detected`, 40P01).
>    Panelden **iptal** ayrı bir işlemdir ve `used_at`'i yeniden kullanamaz
>    ("kod kullanıldı" demektir); M6 gerektirdiğinde kendi sütununu yeni bir
>    migration'da alır. Akış: resolve (bağlamsız) → `WithTenant` → tek ifade;
>    açık `tenant_id` yüklemi **her iki yarıda da** korundu (§4.5 kuşak+kemer).
> 3. **"Kullanıldıktan sonra ölü" = damga, silme DEĞİL.** Tüketim `used_at`
>    damgasıdır; satır **uygulama tarafından silinemez** (`REVOKE DELETE`, ampirik:
>    `has_table_privilege('tappa_app','employee_invites','DELETE')` = `f`), çünkü
>    "hangi kod bu kişiyi ne zaman aktive etti" bir denetim izidir (§4.6).
>    **Garantinin kapsamı yazıldı:** bu `tappa_app`'i bağlar, `tappa_owner`'ı
>    (migration superuser) bağlamaz — onu da kapatmak 00005'teki gibi bir trigger
>    ister, ama 00005'in trigger'ı UPDATE'i de yasakladığı için burada
>    kullanılamaz (`used_at` yazılabilir kalmalı); bilinçli non-goal,
>    `sessions`/`employees` precedent'iyle aynı.
>    **UPDATE artık SÜTUN düzeyinde:** `GRANT UPDATE (used_at)` — tablo geneli
>    UPDATE, dosyanın hiç anmadığı iki yazmaya daha izin veriyordu (süresi geçmiş
>    daveti `expires_at`'i ileri alarak **diriltmek**, `employee_id`'yi aynı tenant
>    içinde **başka çalışana kaydırmak**); üçü de artık `permission denied`
>    (ölçüldü). `sessions`/`tags`'ten bilinçli sapma — orada birden çok meşru
>    yazılabilir sütun var, burada tek. **Kalan sınır:** sütun grant'ı hangi
>    sütunun yazılacağını sınırlar, hangi DEĞERİN yazılacağını değil —
>    `used_at := NULL` (un-consume) hâlâ yetki düzeyinde mümkündür; onu tutan tek
>    şey sorgu disiplinidir, yapısal olması trigger ister (yeni migration).
>    Tek-kullanımlık **atomiktir** (`ConsumeInviteAndActivate`, §4.4 kalıbı);
>    50 goroutine → tam 1 kazanan, ve testin boş yere yeşil olmadığını gösteren
>    **negatif kontrol** aynı dosyada (oku-sonra-yaz 8 kazanan üretiyor).
> 4. **İkinci aktivasyon: veri katmanı karar VERMEZ, ama bir şeyi yapısal olarak
>    kapatır.** `ConsumeInviteAndActivate` `status IN ('invited','active')` ister:
>    zaten aktif çalışanın ikinci aktivasyonu bu katmanda hata değildir (kararı B
>    fazı verir), ama **`deactivated` çalışan bir davetle geri aktive edilemez** —
>    aksi halde müdürün deaktivasyon kararı bir linkle dolanılabilirdi. Ayrıca
>    `activated_at` **ezilmez** (`COALESCE`), ilk aktivasyon damgası korunur.
>
> 5. **"Süresiz davet" iki türlü yazılır; ikisi de kapatıldı, üçüncüsü sınır
>    olarak yazıldı.** `expires_at NOT NULL` yalnız **atlanan** süreyi engeller;
>    ilk taslakta `expires_at = 'infinity'` sorunsuz insert oluyordu ve
>    `now() < 'infinity'` sonsuza dek doğru — yani gerçekten ölümsüz bir davet
>    (ölçüldü, varsayılmadı). `CHECK (expires_at <> 'infinity')` eklendi.
>    **Hâlâ yazılabilen:** sonlu ama saçma bir tarih (9999). O bir **TTL politikası**
>    sorusudur ve TTL kararı B fazınındır; B fazı üst sınırı yapısal isterse **yeni
>    bir migration**'da sınırlı bir CHECK ekler. `-infinity` bilerek serbest: "zaten
>    süresi dolmuş" demek, fail-closed. Test: `TestEmployeeInvites_ExpiryCannotBeAbsent`.
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. Çözümleme sorgusu sayısı 2 → 3 oldu, bu yüzden **ADR 0002 madde 7**
>    güncellendi (karar değişmedi; sayı garanti değil, beş normatif kısıt garanti
>    — üçüncüsü beşini de canlı katalogda karşılıyor).
> 2. **`scripts/redline-check.sh` R7 deseni genişletildi** (güvenlik denetçisi
>    bulgusu): `code[_a-z]*hash` ve yapılandırılmış log anahtarı `"code",` eklendi.
>    Gerekçe: bu tasarımda `code_hash` bir **taşıyıcı (bearer)** kimlik bilgisidir
>    (hash → resolver → tenant → tüketim), yani hash'i loglamak kodu loglamakla
>    aynı ağırlıktadır; eski desen bu satırları **kaçırıyordu** (ölçüldü).
>    **Desen KELİMEYİ değil DEĞERİ hedefler** — ilk deneme çıplak
>    `[^a-z]code[^a-z]` idi ve iki masum satırı (`"reason", "code expired"` ve
>    `fmt.Errorf("… invalid activation code: %w", …)`) FAIL veriyordu; bu, ağın
>    **aşınmasına** (baskı altında gevşetilmesine) davetiyeydi. Yeni desen:
>    dört gerçek sızıntı satırını yakalıyor, iki masum satırı **geçiriyor**, temiz
>    repoda `exit 0` (üçü de ölçüldü). Sınır: `"hash"`/`"c"` gibi bir anahtar
>    altında loglanan değer yakalanmaz (değişken adı `codeHash` ise yakalanır).

> **Kart düzeltmesi (2026-07-31, WiFi adımının veri katmanı).** Kartın WiFi
> maddesi "ağ adı **lokasyon kaydından** gösterilir" diyordu; `locations`
> tablosunda böyle bir alan **yoktu** — kart, şemanın karşılamadığı bir alanı
> varsayıyordu. Kullanıcı kararıyla alan eklendi:
> [00010](../../db/migrations/00010_add_wifi_ssid_to_locations.sql) →
> `locations.wifi_ssid text` (NULLABLE, `CHECK octet_length BETWEEN 1 AND 32`).
> Kriter değişmedi; yalnızca dayandığı alan artık gerçek.
>
> **B fazını bağlayan üç nokta:**
> 1. **Okuma yolu hazır:** `GetLocationWiFi` (`db/queries/locations.sql`) —
>    `tenant_id` + `id` ile, açık tenant yüklemiyle. Lokasyon id'si zaten
>    `ConsumeInviteAndActivate`'in döndürdüğü `e.location_id`'dir; B fazı yeni bir
>    sorgu yazmak zorunda değil.
> 2. **`wifi_ssid IS NULL` = "bu lokasyonun ağı yok, adımı atla"** — hata değil.
>    Boş dize veritabanı tarafından **reddediliyor** (23514), yani "ağ yok"un tek
>    yazımı NULL. Seed'de KF Msida bilerek NULL (nullable yolun fixture'ı).
> 3. **Sütun bir karar girdisi DEĞİL ve olmamalı:** hiçbir policy anahtarı,
>    `tap.Input` alanı veya güven puanı buna dallanmaz; IP kanıtı `static_ips`'ten
>    gelmeye devam eder. SSID istemci beyanıdır — bir hotspot'a istenen ad
>    verilebilir. Şifre/PSK, BSSID/MAC, sinyal gücü ve çevre ağ listesi bilinçli
>    olarak **eklenmedi** (§4.7 yeni sır yüzeyi; §4.1/§4.2 parmak izi ve arka plan
>    konum sinyali). Gerekçeler 00010'un içinde.
>
> **Sınır (yazıldı, iddia edilmedi):** bu üçüncü maddeyi zorlayan hiçbir mekanik
> kontrol yok — bir yorum kısıt değildir. Sütunu bir karar girdisine çevirmeyi
> engelleyen tek şey inceleme.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı sırasında).** Kriterlerin hiçbiri
> düşürülmedi; kartın *anmadığı* yedi karar aşağıda gerekçesiyle sabitleniyor.
> Kart "kod entropisi ≥128 bit" dalını istiyordu: **32 bayt (256 bit)**,
> `internal/invite/code.go` → `codeBytes`. Kısa/insan-okuyan koda geçilmedi, yani
> kilitleme migration'ı gerekmedi — ama şema bunu **zorlamıyor**, sabitin üstündeki
> yorum yükümlülüğü taşıyor (00009 zaten böyle söylüyordu).
>
> 1. **Alan ayrımı iki mekanizmayla: ayrı anahtar + etiketli türetme.** Brief
>    ikisinden birini istiyordu; ikisi de var, çünkü ikisi **farklı** şeye karşı
>    korur. `TAPPA_INVITE_HMAC_KEY` (yeni **zorunlu** env) kriptografik bağımsızlık
>    verir: birinin sızması diğerini forge edilebilir kılmaz. Etiket
>    (`HMAC(key, "tappa/invite/v1|" || code)`) **operasyonel** hatayı kapatır —
>    iki env'i tek `openssl rand` çıktısından kopyala-yapıştır etmek. `config.Load`
>    iki anahtarın **birebir eşitliğini** başlangıçta reddeder
>    (`crypto/subtle.ConstantTimeCompare`), ama eşitlik dışı bir bağımlılık
>    (ör. ikisini tek kaynaktan türeten gelecekteki bir yol) yalnız etiketle
>    zararsız kalır. **Ölçüldü:** aynı anahtar altında invite MAC'i, session'ın
>    çıplak `hex(HMAC(key, value))` yapısına **eşit değil**
>    (`TestHash_DomainSeparation`).
> 2. **Kod, sayfada DEĞİL kısa ömürlü HttpOnly çerezde taşınır.** `GET
>    /activate?code=…` kodu doğrular, `tappa_activation` çerezine yazar ve **temiz
>    `/activate`'e 303** döner. Kazanç ölçülebilir: (a) hiçbir **yanıt gövdesi**
>    kodu taşımaz (test her baytı tarıyor) — gizli input alanı kullansaydık kod,
>    ortak bir dükkân telefonunda render edilen HTML'de, geri-ileri önbelleğinde ve
>    "sayfayı kaydet" çıktısında olurdu; (b) ilk sıçramadan sonra **adres
>    çubuğunda/geçmişte** kod kalmaz; (c) `SameSite=Lax` sayesinde çapraz-site POST
>    çerezi taşımaz → **forge edilmiş POST** kapalı (çerez EKİMİ değil — bkz.
>    aşağıdaki 2. tur bloğu). **Bedeli açıkça yazıldı:**
>    çerezi reddeden tarayıcı aktive olamaz — ama ürün zaten kalıcı çereze
>    dayanıyor, bu yüzden hata **erken ve açıklanabilir** hâle gelir (plaket
>    başında değil). Çerez tipi `insecure bool` kutbuyla yazıldı (M5-01 3. tur
>    dersi): **sıfır değer Secure**.
> 3. **Üçüncü uç nokta: `GET /activate/done`.** POST 303 ile buraya yönlendirir ve
>    burası ziyaretçiyi **yeni oturum çerezinden** tanır. İki sebep: (a) PRG —
>    yenilenen sayfa tüketilmiş bir kodu tekrar POST etmez; (b) çerezin gerçekten
>    saklandığı **tam da o anda** ölçülür, ertesi gün plakette değil.
> 4. **İkinci aktivasyon: yeni cihaz kabul, önce iptal sonra üretim.** Zaten
>    `active` çalışan + geçerli kullanılmamış davet = yeni telefon.
>    `RevokeAllForEmployee` **`Issue`'dan ÖNCE** çağrılır — sıra taşıyıcıdır, çünkü
>    o sorgu çalışanın **tüm canlı** oturumlarını iptal eder ve sonra çağrılsaydı
>    yeni oturumu da öldürürdü (`TestSubmit_SecondDeviceRevokesBeforeIssuing`
>    sırayı ölçüyor). Aktivasyon öncesi form **uyarır** ("This is a new phone…"),
>    sonrası onay ekranı **söyler**, `audit_log`'a `activation.device_replaced`
>    yazılır. Gerekçe: çalınan telefon senaryosunda tek çare budur ve M5-01
>    `RevokeAllForEmployee`'nin meşru çağıranları arasında bunu zaten sayıyor.
>    ⚠️ Deaktivasyon bu yolu **çağırmıyor** (M5-01 3. tur eki) — dokunulmadı.
> 5. **Oran sınırı iki katman ve biri bilinçli olarak izsiz.** (a) **IP başına**,
>    DB'ye dokunmadan — yük atmanın tek mümkün yeri; tenant **bilinmediği** için
>    `audit_log`'a yazılamaz (tablo `tenant_id NOT NULL` + FK; "sistem tenant'ı"
>    uydurmak daha kötü olurdu). Bu boşluk yazıldı, kapatılmış gibi yapılmadı.
>    (b) **davet başına**, davet UUID'siyle anahtarlanır (kod/hash **asla** bellek
>    map'inin anahtarı olmaz) — tenant bilindiği için 429 **`audit_log` satırı
>    bırakır**. **Yalnız başarısızlıklar sayılır**, yani meşru akış (bir link, bir
>    form, bir onay) sınıra yapı gereği değmez — bu, "büyük sayı seçmek" yerine
>    ölçülen bir özellik (`TestRateLimit_SuccessNeverConsumesBudget`). M5-03'ün
>    geniş tap sınırı bu uçları **kapsamıyor**, kartın istediği gibi.
>    ⚠️ **Bu madde 4. turda ikiye bölündü ve bir cümlesi çürütüldü** — aşağıdaki
>    4. tur bloğuna bak: "meşru akış sınıra yapı gereği değmez" akışın **kendi
>    katkısı** için doğru, **servis edilip edilmediği** için değil.
> 6. **`TAPPA_RETENTION_YEARS` zorunlu, aralık [1,30].** Aralık **hukuki değil
>    mantık** sınırıdır (0 = "0 yıl saklanır" yazan bir aydınlatma metni; 500 =
>    "sonsuza dek"). Üretim dışı dağıtımlarda sayfa metni bunun **konfigürasyondan
>    okunan bir placeholder** olduğunu açıkça söylüyor (Q13 / backlog B3 hâlâ
>    açık). Kodda hiçbir yerde gömülü yıl sayısı yok.
> 7. **Q02 kanalı arayüz ardında ve iz bırakıyor.** `invite.Channel` tek egress;
>    tek implementasyon `ManagerVisibleChannel`, adı bilinçli olarak rahatsız edici
>    ve **her teslimde `invite.code_shown_to_manager` audit satırı** yazıyor —
>    ADR 0005 Y-D'nin tespit sinyali (M6-11) böylece ham veriye kavuşuyor.
>    `IssueAndDeliver` kodu **çağırana döndürmez**.
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. **Yeni sorgu:** `db/queries/employees.sql` → `GetEmployeeActivationContext`
>    (isim + durum + lokasyon + **tenant adı** = GDPR veri sorumlusu). Kartın
>    "B fazı yeni sorgu yazmak zorunda değil" notu **yalnız SSID okuması** içindi;
>    SSID gerçekten `GetLocationWiFi` ile okunuyor. Bu dosyada **yazma sorgusu
>    yok** — A fazının "aktive eden tek ifade" garantisi korundu.
> 2. **Yeni sorgu:** `db/queries/audit.sql` → `RecordAuditEvent` (§4.6; tablo
>    vardı, INSERT sorgusu yoktu). Yeni paket `internal/audit`.
> 3. **Yeni paketler:** `internal/invite`, `internal/audit`, `internal/handler`
>    (ilk sakini), `web/templates/{layout,pages}` (ilk `.templ` dosyaları) →
>    `go.mod`'a `github.com/a-h/templ` eklendi, `make audit` koşuldu (M1-07→M1-09
>    dersi): `No vulnerabilities found`.
> 4. **`httpx.NewRouter` imzası değişti:** `NewRouter(cfg, features ...Mounter)`.
>    `cmd/tappa` artık havuzu ve yöneticileri kuruyor (wiring; iş mantığı yok).
>
> **Kapsam DIŞI bırakılanlar (bilinçli):**
> - **Davet üreten HTTP uç noktası YOK.** Panel M6, admin kimlik doğrulaması da
>   M6 — kimliksiz bir "davet üret" ucu, bu görevin azaltmaya çalıştığı Y-D
>   riskini doğrudan internete açardı. Davet bugün yalnız `invite.Manager` +
>   `Channel` üzerinden üretilebiliyor.
> - **M5-03 middleware'i yazılmadı:** istemci IP'si `r.RemoteAddr`'ın host'u,
>   `X-Forwarded-For` **okunmuyor**. Ters proxy arkasında IP penceresi tüm mekân
>   için ortak olur — bu yüzden IP penceresi geniş, davet penceresi dar.
> - **§5 satır 3 tam bağlanmadı:** yönlendirmeyi yapacak `GET /t` M5-04'te.
>   Teslim edilen, **hedefin var ve erişilebilir** olması: `/activate` çalışıyor ve
>   hiçbir `transactions` satırı yazmıyor. (Tüketilmiş kodla gelen bir istek
>   `audit_log`'a yazar — §4.6 gereği doğru; eski "hiçbir kayıt yazmıyor" cümlesi
>   3. turda ölçülerek çürütüldü.) "Bağlandı" denmiyor.
> - **M5-07 turu ve practice tap** bu görevde yok.
> - **Yazı tipleri hâlâ self-host EDİLMİYOR:** `web/static/fonts/` dizini yok,
>   `app.css`'te **sıfır** `@font-face` (ölçüldü) — M5-02 öncesinden gelen boşluk.
>   Kırmızı çizgi korunuyor — sayfa hiçbir dış kaynağa bağlanmıyor (render edilen
>   HTML'de tek `href` `/static/css/app.css`), ama Space Grotesk / IBM Plex Mono
>   gelene kadar tarayıcı **sistem yazı tipine düşer**. `base.templ` bir süre
>   tersini söylüyordu; iddia gerçeğe indirildi (2. tur). M5-04 ile kapanmalı.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 2. tur — üçüncü göz RED sonrası).**
> Denetim iki **bloklayan** bulgu ölçtü; ikisi de aynı sınıftan: *dosya,
> sağlamadığı bir güvenlik sınırını beyan ediyordu.*
>
> **B1 — aktivasyon-fixation. "SameSite=Lax sayesinde CSRF yapısal olarak kapalı"
> YANLIŞTI ve çapraz-tenant render ÖLÇÜLDÜ.** SameSite çerez **yazmayı**
> kısıtlamaz, yalnız **göndermeyi**. Denetçi kurbanın tarayıcısını çapraz-site
> `GET /activate?code=<saldırganın kodu>` yaptırdı → `Set-Cookie` yerleşti →
> sonraki aynı-site GET **başka tenant'ın** formunu render etti → aynı-site POST
> (Lax bunu **taşır**) kurbanın telefonunu yabancı bir çalışan kaydına bağladı ve
> **kurbanın oturum çerezini sessizce ezdi**. Saldırı bu turda **yeniden üretildi**
> (üç adım da başarılı), sonra üç önlem eklendi ve **yeniden koşuldu** (üç adım da
> başarısız) — `internal/handler/activate_test.go` → `TestB1_*`:
>
> 1. **Synchronizer token.** Aktivasyon çerezi artık `<csrf>.<code>`; form
>    token'ı echo ediyor, `Submit` `crypto/subtle.ConstantTimeCompare` ile
>    karşılaştırıyor. Token **koddan türetilmiyor** (türetilseydi saldırgan
>    hesaplardı) ve sunucuda üretildiği için saldırgan onu **okuyamaz**. Geriye
>    kalan **kimlik avıdır**, CSRF değil — ve o sayfada **başkasının adı** yazar.
> 2. **`Submit` mevcut oturumu görüyor.** İstek canlı bir `tappa_session`
>    taşıyorsa ve aktive edilecek çalışan **o oturumun çalışanı değilse**: form
>    **409** ile geri döner, mevcut sahip **adıyla** yazılır ve ayrı bir onay
>    kutusu (`switch`) istenir. Sessiz ezme kalktı — kurbanın oturumu bir
>    **kanıttır**. `audit_log`'a `activation.blocked` düşer.
> 3. **Çapraz-site GET, dolu telefonda çerez ekemiyor.** `Sec-Fetch-Site:
>    cross-site` **ve** telefonda başka birinin canlı oturumu varsa çerez
>    **yazılmıyor**; aynı-site bir onay adımı (`POST /activate`, Origin kontrolü
>    **katı**) gerekiyor. ⚠️ **Kasıtlı ve ölçülmüş sapma:** koordinatör
>    "çapraz-site GET'te çerez ekme" dedi; **koşulsuz** uygulanmadı, çünkü ürünün
>    **normal** akışı (WhatsApp/SMS/e-postadaki linke dokunmak) **daima
>    çapraz-site**'tir — koşulsuz interstitial her aktivasyona bir tık ekler **ve**
>    kodu her ilk sayfanın gövdesine sokardı (denetimin doğruladığı "gövdede kod
>    yok" özelliğini bozardı). Kural bu yüzden **çatışma varken** uygulanıyor:
>    ölçülen saldırının tam şekli budur (kurbanın oturumu var). Pozitif kontrol
>    testte: aynı istek çapraz-site başlığı olmadan hâlâ çerezi yazıyor.
>    **Sınır:** `Sec-Fetch-Site` her tarayıcıda yok ve **yokluğu aynı-site
>    sayılıyor** (fail-open) — bu yüzden 1 ve 2 asıl korumadır, 3 ektir.
>
> **Ek olarak `Origin` kontrolü:** `POST /api/activate`'te gevşek (`Origin` varsa
> eşleşmeli; yoksa token'a güveniliyor) ve `POST /activate`'te **katı** (eşleşme
> ya da `Sec-Fetch-Site: same-origin` şart). Gerekçe: `Origin` çapraz-origin
> POST'ta her modern tarayıcıda gönderilir, yani form gönderimi için **gerçek**
> bir kontroldür; `Sec-Fetch-Site` değildir.
>
> **B2 — `base.templ` "fontlar self-host ediliyor" diyordu; edilmiyor.** Ölçüm:
> `web/static/fonts/` yok, `@font-face` sayısı **0**. Dosyanın iddiası gerçeğe
> indirildi (dış istek yok = kırmızı çizgi duruyor; marka fontu yok = sistem
> fontuna düşülüyor; self-host **M5-04**). Kart ile dosya artık aynı şeyi diyor.
>
> **B3 — kartın kendi içindeki çelişki** giderildi: §5 satır 3 kriteri artık
> yönlendirmenin **M5-04**'e ait olduğunu söylüyor.
>
> **B4 — onay kutusunu unutmak artık kullanıcının kendi davet bütçesini
> yakmıyor.** `failAttempt` bir **bütçe kapsamı** aldı: `consent_missing` yalnız
> (geniş, 60'lık) IP penceresini artırıyor; kötüye kullanım sinyalleri (yanlış
> form token'ı, tüketilmiş/süresi geçmiş kod, aktive edilemez çalışan) davet
> penceresini artırmaya devam ediyor. Ölçüldü: 30 arka arkaya onay hatasından
> sonra çalışan **hâlâ** aktive olabiliyor, ve pozitif kontrol olarak tüketilmiş
> kodu 11 kez denemek hâlâ **429** veriyor (`TestB4_*`). Eski iddia ("meşru akış
> sınıra yapısal olarak değmez") yalnız **hatasız** akış için doğruydu.
>
> **B5 — sabit pencere (fixed window) sınırı yazıldı** (`ratelimit.go`): pencere
> sınırında kısa sürede 2×limit mümkün. İddia edilmiyor, **sınır** olarak duruyor;
> kayan pencere/token bucket paylaşılan depoyla birlikte M8'de değerlendirilir.
>
> **Yeni uç nokta:** `POST /activate` (yalnız çatışma onayı). Yeni audit eylemi:
> `activation.blocked`. Yeni görünüm: `pages.Confirm` — **bu sayfa kodu gizli
> alanda taşır** ve paketin "gövdede kod yok" kuralının **bilinçli tek
> istisnasıdır**: yalnız çatışmalı çapraz-site gelişte render edilir; o durumda kod
> ya ziyaretçinindir ya saldırganın (zaten bildiği). Normal akış bu sayfayı hiç
> görmez.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 3. tur — yeni üçüncü göz RED
> sonrası).** 2. turun B1 düzeltmesi bağımsız denetimde **doğrulandı ve
> non-vacuous kanıtlandı** (denetçi üç önlemi tek tek sabote edip testleri
> kırmızıya döndürdü, sonra dosyaları sha256 ile geri yükledi). Bu turda **iki
> yeni bloklayan** çıktı.
>
> **🔴 1. Sınırsız, SİLİNEMEZ `audit_log` yazımı.** İki dal (`overInviteBudget`
> ve `recordConflict`) her istekte bir satır yazıyor, **hiçbir pencereyi
> artırmıyordu**. Ölçüldü — önce/sonra:
>
> | Sonda | Önce | Sonra |
> |---|---|---|
> | 300 × `GET /activate?code=<tüketilmiş>` | 290×429, **300 satır** | 290×429, **60 satır** |
> | 400 × `POST /api/activate` (ölü kod) | 390×429, **400 satır** | 390×429, **60 satır** |
> | 500 × çapraz-site GET (çakışan oturum) | **0×429**, **500 satır** | **440×429**, **60 satır** |
>
> Ön koşul yalnızca **ölü bir davet linki** — yani aktive olmuş her çalışanın
> telefonunda duran kendi linki; oturum, kimlik, geçerli kod gerekmiyor. Ve
> `audit_log` **veritabanı seviyesinde** append-only (`tappa_owner` bile
> `DELETE` alamıyor), yani satırlar **kalıcı**. Bu, §4.6'nın korumaya çalıştığı
> izin kendisine karşı bir servis engelleme saldırısıydı. **Düzeltme:** her iki
> dalda `a.ipLimiter.fail(ip)`. `ratelimit.go`'nun "yalnız başarısızlıklar
> sayılır" ve "DB işinden önce" cümleleri de gerçeğe indirildi. **429'un izi
> kaybolmuyor:** WARN log satırı duruyor (IP-başına aşımın audit satırı olmaması
> §4.6 ihlali değildir — denetçiyle mutabık). Pozitif kontrol: 200 ardışık
> **başarılı** aktivasyon hiç kısıtlanmıyor.
>
> **🔴 2. M5-01'in RED'i yeni pakette yeniden üretilmişti.**
> `handler.activationState.code` **çıplak `string`**'di; ölçüldü: `%v`/`%+v`/
> `%#v` ham kodu bastı — sarmalayıcı struct'ta da. `invite.Code`'un `*string`
> dolaylılığı, değer tipinden çıkarıldığı anda düşüyordu. **Düzeltme:** alan artık
> `invite.Code`; ham dize yalnızca iki istek giriş noktasında ve `set`'in tek
> satırında **fonksiyon-yerel** olarak var. Mutasyon kanıtı: alanı `string`'e
> çevirince yeni test **7 render yolunda** kırmızı (`TestActivationState_*`).
>
> **Bloklamayan beş madde de kapatıldı:**
> 1. **`heldBy` artık DB hatasında fail-CLOSED.** Önce ölçülmüştü: `Verify` hata
>    verince `Submit` 409'u atlayıp oturum üretiyordu — koruma tam da sistem
>    hastayken kendini kapatıyordu. Artık "bilmiyorum" ≠ "oturum yok": 500 +
>    "tekrar dene", hiçbir şey tüketilmiyor. Mutasyonla kanıtlandı.
> 2. **Adres çubuğu iddiası indirildi:** çakışmalı çapraz-site varış 303 değil
>    **200** döner, yani o URL (kod dahil) geçmişte kalır. İstisna `cookies.go`'da
>    yazılı.
> 3. **`view.go` kendi içindeki çelişki giderildi:** `ConfirmView.Code` artık
>    paket dokümanında **adıyla anılan tek istisna**.
> 4. **"Bir kod harcanana kadar hiçbir kayıt yazmıyor" cümlesi** hem kodda hem
>    kartta düzeltildi (yukarıdaki kabul kriteri): yazılan `audit_log` satırı
>    §4.6 gereği **doğru**, yanlış olan cümleydi.
> 5. **Önlem 3'ün koşulluluğu ve M5-04 devri yazıldı.** Oturumsuz telefonda
>    çapraz-site GET hâlâ çerez ekiyor; bugün saldırgana **ek yetenek
>    kazandırmıyor** (aynı linki doğrudan da gönderebilirdi) ama **M5-04** `GET /t`
>    oturumsuz tap'i `/activate`'e yönlendirdiğinde bu ekili çerez o yolun girdisi
>    olur. Risk `cookies.go`'da **M5-04 adıyla anılarak** yazıldı; bugünkü azaltım
>    formun **hangi işletme + hangi çalışan** için aktive olduğunu hem üstte hem
>    **butonun hemen üstünde** söylemesi. Ayrıca `Sec-Fetch-Site` karşılaştırması
>    **büyük/küçük harf duyarsız** yapıldı — ölçülmüştü: `Cross-Site` ve
>    `CROSS-SITE` çerezi ekiyordu (M5-01'in `HTTPS://` hatasıyla aynı sınıf).
>
> **B2 iki dosyada daha düzeltildi:** `web/static/css/input.css` ve
> **`.claude/skills/tappa-brand/SKILL.md`** — skill bir **spec**'tir ve yanlış bir
> spec sonraki UI ajanını yanıltır (agent-brief md. 6). Üçü de artık aynı şeyi
> diyor: dış bağlantı yok (kırmızı çizgi duruyor), marka yazı tipleri **henüz
> yok**, sahibi **M5-04**.
>
> **Bileşen tekrarı giderildi:** `web/templates/components/` açıldı
> (`Notice` + `Card`); `border-l-4 border-tomato bg-paper px-4 py-4` zinciri
> `activate.templ`'de **5 → 1**.
>
> **Kapsam dışı bırakılan, sınır olarak yazılan:** **çerez gölgeleme** — bir alt
> alan adı aynı isimle ikinci bir `tappa_activation` yazabilir ve `r.Cookie`
> ilkini alır. Standart çare `__Host-` önekidir; `internal/session` onu
> `http://localhost` geliştirmeyi bozduğu için **bilinçli olarak** ertelemişti.
> Hangi hostların bu alan adı altında var olacağı bir **dağıtım** kararıdır → M8.
> Kod değiştirilmedi, yalnız `cookies.go`'ya yazıldı.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 4. tur — `tappa-security-auditor`
> ONAY + son bulgular).** Güvenlik denetimi 3. turun iki bloklayanını kendi
> ölçümüyle kapalı buldu (300 GET → **tam 60** satır; 500 çapraz-site GET →
> **440×429 + tam 60** satır; `%+v` matrisinde `invite.Code` her verb'de redacted,
> pozitif kontrol ham dizenin sızdığını gösteriyor). Bu turda **bir ana bulgu ve
> beş küçük madde** kapatıldı.
>
> **⚠️ Oran sınırı BAŞARILARI da reddediyordu — bütçeler ayrıldı.** Tek bir per-IP
> penceresi her handler'ın ilk kontrolüydü, yani **geçerli** bir aktivasyonu da
> reddediyordu. Ölçüldü: **60 × bilinmeyen kod** (kimlik, oturum, geçerli kod
> gerektirmez) → ardından **geçerli** kodla gelen çalışan **429** alıyor, hâlâ
> `invited`. Ağırlaştıran: `clientIP` bilinçli olarak `X-Forwarded-For` okumuyor
> ve dağıtım **tek VPS + ters proxy**, yani proxy arkasında **tüm internet tek
> anahtarı** paylaşır — 0.1 istek/sn ile ürünün aktivasyonu süresiz kapatılabilir.
>
> **Üç bütçe, üç iş** (`ratelimit.go`):
>
> | bütçe | anahtar | neyle artar | neyi reddeder |
> |---|---|---|---|
> | `flood` (600/10dk) | IP | **her istek** | gerçek DoS — **geçerli aktivasyonu reddedebilen tek bütçe** |
> | `unknown` (60/10dk) | IP | **tenant'sız** başarısızlıklar (bilinmeyen kod, çerez yok, yabancı Origin) | hiçbir şeyi — yalnız **süreç log'unu** sınırlar |
> | `invite` (10/10dk) | davet UUID'si | **atfedilebilir** başarısızlıklar | `audit_log` satırlarını sınırlar |
>
> **Önce/sonra ölçüm** (aynı sondalar):
>
> | sonda | 3. tur | 4. tur |
> |---|---|---|
> | 300 × `GET` (ölü kod) | 290×429, 60 satır | 290×429, **11 satır** |
> | 400 × `POST` (ölü kod) | 390×429, 60 satır | 390×429, **11 satır** |
> | 500 × çapraz-site GET | 440×429, 60 satır | 490×429, **11 satır** |
> | 60 bilinmeyen → 1 **geçerli** kod | **429** (kilitli) | **303 (servis ediliyor)** |
> | 605. istek, tek adres | — | **429** (kalkan hâlâ ısırıyor) |
>
> Bloklayan #1'in kazanımı **bozulmadı, iyileşti** (60 → 11): `overInviteBudget`
> artık satırı yalnız **pencere eşiğini geçen ilk istekte** yazıyor
> (`limiter.firstOverLimit`), `recordConflict` ise davet penceresini artırıyor.
> **Bu sınırın kapsamı yazıldı:** davet **başına**dır; N farklı geçerli davet
> tutan bir çağıran N× yazabilir ve onu sınırlayan `flood` tavanıdır — yani
> satır sayısı asla istek sayısını aşmaz.
>
> **Çürütülen iddia gerçeğe indirildi** (`ratelimit.go` + yukarıdaki 5. madde):
> "meşru akış sınıra **yapı gereği** değmez" akışın **kendi katkısı** için
> doğrudur (200 ardışık başarılı aktivasyon → sıfır 429), ama "akış **her koşulda
> servis edilir**" demek **değildir** — `flood` tavanı aynı kaynak adresten gelen
> herkese uygulanır.
>
> **🔴 M5-03'e ADLANDIRILMIŞ DEVİR — kapatılmadı, kapatılamaz.** Gerçek istemci
> IP'si (`X-Forwarded-For` + `cfg.TrustedProxies`) çözülene kadar: (a) `flood`
> tavanı per-caller değil **global**'dir → dışarıdan biri onu harcayıp herkesin
> aktivasyonunu pencere boyunca engelleyebilir; (b) `unknown` bütçesinin log
> susturması da global'dir. Bütçe ayrımı bunun **ucuz** biçimini kaldırdı (altmış
> kötü link artık gerçek bir linki reddetmiyor); geri kalanı burada ayarlanabilir
> bir sayı değil, **M5-03'ün işi** — ve M5-03 `floodLimit`'i de yeniden
> değerlendirmeli (per-adres trafiğe göre seçildi). Aynı uyarı
> `activate.go` → `clientIP` ve `ratelimit.go` başlığında da yazılı.
>
> **Beş küçük madde:**
> 1. **`cookies.go` ile `view.go` çelişkisi giderildi.** `pages.ConfirmView.Code`
>    **exported düz `string`**'dir ve `%+v` onu **basar** (ölçüldü; aynı koşuda
>    `invite.Code` redacted). **Redakte edilemez:** templ değeri gizli alana
>    yazarken tipin dize biçimini ister — redakte eden tip "…(redacted)" post
>    ederdi. Bu yüzden istisna **ilan edildi**, tek görünümde, tek yolda; koruma
>    tip sistemi değil **inceleme** ve bu artık her iki dosyada da böyle yazılı.
> 2. **"Normal akış bu sayfayı hiç görmez" YANLIŞTI** (ölçüldü): **ortak dükkân
>    telefonu** — biri oturum açıkken ikinci çalışanın **kendi** linkini sohbet
>    uygulamasından açması (daima çapraz-site) tam da bu sayfayı render eder. Yani
>    "URL'de kod kalıyor" istisnası nadir değil; düzeltildi.
> 3. **Origin-red dalları artık ücretsiz değil:** `unknown` bütçesini artırıyorlar
>    ve log satırı bütçe dolunca **susuyor** (bir kez "susturuyorum" diyerek).
>    Ölçüldü: 400 istek → 60+ değil, **sınırlı** satır.
> 4. **`GET /activate/done` kalkanın arkasına alındı** — yazma yok ama istek başına
>    iki DB okuması var; `ratelimit.go`'nun "DoS kalkanı" cümlesi artık akışın
>    tamamını kapsıyor.
> 5. **§4.6 asimetrisi giderildi:** `cookies.Set` hatası artık `sessions.Issue`
>    hatası gibi `audit_log`'a yazıyor — iki dal **aynı durumu** bırakıyor (davet
>    tüketilmiş, çalışan `active`, kullanılabilir oturum yok). Pratikte erişilemez
>    bir dal; zaten bu yüzden asimetri incelemeden geçmişti.

---

## M5-03 — Middleware: gerçek IP, tenant, oran sınırı

- **Bağımlılık:** M1-07 · M5-01
- **Kırmızı çizgi:** §4.5
- **Commit:** `feat(httpx): add real-ip, tenant and rate-limit middleware`

**Amaç.** İstek sınırında bağlamı kurmak: kim, hangi tenant, hangi IP.

**Kabul kriterleri.**
- Gerçek IP yalnızca `cfg.TrustedProxies` içindeki hop'lardan gelen
  `X-Forwarded-For` ile çözülüyor; dışarıdan gelen başlık **yok sayılıyor**
  (aksi halde *proof of place* uydurulabilir).
- Tenant bağlamı `WithTenant` üzerinden; handler'da elle `SET` yok.
- Oran sınırı: tap uçları için IP + oturum bazlı. Sınır, meşru bir tap'in **asla
  değemeyeceği** kadar geniş olmalı; aşan istekler `429` alır ve `audit_log`'a
  yazılır — sessizce düşürülmez. (§4.6 "kayıt asla kaybolmaz" ile çelişmemesi
  için: sınır bir kötüye kullanım kalkanıdır, meşru mesaiyi eleyen bir filtre
  değil. Debounce zaten guardrail'de.)
- Panik kurtarma zaten var; 500 dönerken tap kaydı kaybolmuyor mu — M5-05'te
  ayrıca ele alınır.

**Tuzaklar.**
- `middleware.RealIP` chi'de başlığa **koşulsuz** güvenir. Tappa'da bu yeterli
  değil; `TrustedProxies` ile sınırlı kendi middleware'ini yaz veya RealIP'i
  yalnız güvenli dağıtımda kullan. Bu, denetimde R5 kapsamına girer.

---

## M5-04 — `GET /t`: tap sayfası

- **Bağımlılık:** M2-07 · M5-01 · M5-03
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(tap): serve the employee tap page`

**Amaç.** Plakete dokunulduğunda açılan tek ekran.

**Kabul kriterleri.**
- "Hello Maria" + lokasyon adı + **tek** buton (≥64px yükseklik, ıslak/eldivenli
  parmakla basılabilir).
- Menü, sekme, ayar **yok**.
- Sunucu tarafında SUN ön-doğrulaması yapılır; sayfa açılırken sayaç **ilerletilmez**
  (ilerletme `POST /api/checkin` anında — aksi halde sayfayı açıp basmayan kullanıcı
  sayacı harcar).
- GPS yalnızca butona basıldığında `getCurrentPosition` ile okunur —
  `watchPosition` **yasak** (§4.2).
- Oturum yoksa aktivasyon sayfasına yönlendirir, **kayıt yazmaz**.
- Fontlar `web/static/fonts/` altından self-host; Google Fonts'a runtime bağlantı yok.

**Tuzaklar.**
- Sayfada `ctr`/`cmac` değerlerini JS'ye veya DOM'a gereksiz yere gömme; POST'a
  sunucu tarafında imzalı bir bağlam taşımak daha temiz.
- Karanlık tema yok (şimdilik) — yarım iş yapma.

---

## M5-05 — `POST /api/checkin`: orkestrasyon

- **Bağımlılık:** M2-07 · M4-07 · M5-04
- **Kırmızı çizgi:** §4.3 · §4.4 · §4.6 — **milestone'un en kritik görevi**
- **Commit:** `feat(tap): decide and record check-in`

**Amaç.** Dört kanıtı toplayıp karara bağlamak ve kaydı yazmak.

**Akış.** parse → `sun.Verify` → bağlam topla (oturum, IP, GPS, vardiya, son
işlemler) → `tap.Decide` → **kaydı yaz** → yanıt üret. Handler'da iş kuralı ve
ham SQL **yok** (§3).

**Kabul kriterleri.**
- Karar ne olursa olsun (`ok`/`flag`/`reject`/`ignored`) **kayıt yazılır** —
  tek istisna §5 satır 3 (oturum yok).
- Hata yolunda kayıt kaybı yok: DB yazımı başarısızsa istemciye "Try again"
  dönülür ve olay log'lanır; sessiz yutma yok (§7).
- Sayaç ilerletme CMAC doğrulamasından **sonra**, tek atomik ifadede.
- Yanıt DTO'su `internal/handler`'da; `internal/store` tipleri API'ye sızmıyor.
- Log'da: oturum token'ı, CMAC, AES anahtarı, davet kodu, **tam GPS** yok (§7).
- Deaktive çalışan denemesi → `reject` + güvenlik uyarısı + audit izi.
- **`channel='manual'` yazımında `entered_by` ZORUNLU** (M4-06'dan devredildi):
  boşsa yazma yolunda **hata** — sessiz kabul yok. `tap.Decide` saf ve imzası
  sabit olduğu için (hata döndüremez) bu doğrulama burada, handler sınırında yapılır
  (§7); `entered_by` bir köken alanıdır, karar girdisi değil. Manuel giriş UI'ı ve
  otomatik doldurma M6-04.

- **`occurred_at` istemciden geliyorsa guardrail'e tabidir.** `sys:occurred-at-bound`
  ([M3-05](m3-policy-motoru.md)): gelecek zaman → `deny`; `created_at − occurred_at`
  sapması üst sınırı (0–72 sa) aştıysa → `deny`. Sınır **kapatılamaz**, yalnız
  aralık içinde ayarlanır.
- `tap:queued` **istemci beyanından değil**, sunucudaki `occurredAtSkewSeconds`
  farkından türetilir.

**Tuzaklar.**
- `flag` kararını sonradan `ok`'a çeviren bir UPDATE yazma — onay ayrı tabloya
  yazılır (§4.3, Q20, M6-04).
- Çevrimdışı kuyruktan gelen geç POST'lar: `occurred_at` istemciden gelir,
  `created_at` sunucudan. İmzalı `ctr` geç senkronda da geçerlidir. Kuyruk
  istemcisi M9-01'de; sunucu tarafı bu ayrımı **şimdiden** doğru kurmalı.
- **Ama sınırsız bırakma (K1).** Zaman dört kanıtın hiçbirinde yok: SUN
  payload'ında zaman yok, IP/GPS/oturum zamansız. Yani `occurred_at`, mesai
  kaydının **en önemli alanı** olmasına rağmen tek doğrulanmamış istemci
  girdisidir. Çalışan 09:00'da gerçekten tap yapar (SUN ✓, IP ✓, tazelik ✓) ama
  gövdeye `occurred_at=07:00` yazar → 2 saat sahte mesai. Sınırı yalnız
  `base:queued-window`'a bırakmak yetmez: o bir **baseline**'dır, tenant
  kapatabilir. Üst sınır guardrail'de olmak zorunda.

---

## M5-06 — Onay ekranı ve marka mesajları

- **Bağımlılık:** M5-05
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(tap): add confirmation screen with tenant messages`

**Amaç.** İşlem sonrası ekran.

**Kabul kriterleri.**
- Başarılıda **buton yok**: *"All done — you can close this page."*
- Başarısızda **"Try again" var**.
- Tenant'a özel mesaj gösteriliyor (KF/KM metinleri skill'de; panelden
  düzenlenebilirlik M9-04).
- Durum rengi sabit eşlemeye uyuyor: `ok → tappa-green`, `flag → saffron`,
  `reject → tomato`, `ignored → line`.
- Durum **yalnız renkle** anlatılmıyor; damga metni de var (erişilebilirlik).
- Saat ve süre **mono** (IBM Plex Mono).

**Tuzaklar.**
- "Bir de şunu ekleyelim" baskısına direnç: bu ekran ürünün en sade parçası ve
  öyle kalmalı. Değişiklik önerisi → önce sor.

---

## M5-07 — Mini tur ve practice tap

- **Bağımlılık:** M5-02 · M5-06
- **Commit:** `feat(activation): add three-slide tour and practice tap`

**Amaç.** Operasyonel onboarding'i sıfırlamak: eğitim kayıt anında tamamlanır.

**Kabul kriterleri.**
- Aktivasyondan hemen sonra 3 slayt: "Tap the plaque" → "One button" →
  "First tap is practice".
- İlk check-in `practice = true`, **TRAINING** damgası (ink üstüne kesikli çerçeve).
- Practice kaydı çalışılan saate **asla** sayılmıyor ve yön zincirine girmiyor
  (M4-06 ile tutarlı).
- Tur atlanabiliyor ama practice bayrağı yine ilk kayda düşüyor.

---

## M5-08 — QR kanalı

- **Bağımlılık:** M5-05
- **Commit:** `feat(tap): support the qr fallback channel`

**Amaç.** NFC'siz/kapalı telefonlar için yedek yol.

**Kabul kriterleri.**
- QR statik → `ctr`/`cmac` yok → `sun_valid = false`, `channel = 'qr'`.
- **Bu kanalda IP zorunlu; GPS tek başına yetmez → `flag`** (Q15 kararı,
  `base:qr-requires-ip`). Gerekçe: QR fotoğraflanır, süresiz geçerlidir ve
  hiçbir fiziksel dokunuş kanıtı taşımaz — tek gerçek kanıt mekânın ağında olmak.
- QR asla NFC ile aynı trust seviyesine çıkmıyor.
- Plaket baskısında NFC + QR birlikte — tasarım notu `tappa-brand` ile uyumlu.
- **QR bazı çalışanlar için kalıcı yoldur, yedek değil.** iPhone X ve öncesi
  arka planda NFC etiketi okuyamaz (arka plan okuma iPhone XS/XR ve sonrası) —
  o telefonlardaki çalışanlar için URL hiç açılmaz. Q15 gereği QR'da IP zorunlu
  olduğundan, bu çalışanlar mekânın WiFi'ına bağlanmazsa **her gün** `flag`
  üretir. Pilot öncesi telefon envanteri çıkarılır ([M8-07](m8-deploy-pilot.md));
  oran yüksekse Q15 varsayılanı tenant bazında yeniden değerlendirilir.

---

## M5-09 — Uçtan uca entegrasyon testi ve "bir günü simüle et"

- **Bağımlılık:** M5-05 … M5-08 · M1-10
- **Kırmızı çizgi:** §8
- **Commit:** `test(tap): add end-to-end flow and full-day simulation`

**Amaç.** Gerçek Postgres'e karşı, gerçek HTTP üzerinden akışı kanıtlamak; ve
dashboard'un üstünde duracağı gerçekçi bir günü üretmek.

**Kabul kriterleri.**
- Aktivasyon → practice tap → normal in → out zinciri uçtan uca yeşil.
- KF St Julians "bir günü simüle et": 10 çalışan, ~21 işlem, skill `tappa-seed`
  tablosundaki **her senaryo** dahil (geç kalma, çift tap, ardışık farklı kişiler,
  mobil veri, flag, deaktive deneme, retired plaket, QR, practice, manuel,
  Rusty Bar gece vardiyası).
- Üretilen kayıtlar **karar motorunun çıktısıyla** oluşuyor — `verdict`, `trust`,
  `ip_match` elle uydurulmuyor. Tutarsız veri hatalı motoru doğru gösterir.
- `-race` ile temiz.
- Milestone kapanışı: agent `tappa-security-auditor` tam denetim, `make audit` temiz.

---

## M5-10 — Tap tazelik penceresi (URL biriktirmeye karşı)

- **Bağımlılık:** M5-04 · M5-05 · M3-05 (guardrail)
- **Kırmızı çizgi:** §4.4 (replay koruması) · §4.6
- **Commit:** `feat(tap): bind a tap url to a freshness window`

**Amaç.** Açılmış bir tap sayfasının süresiz geçerli kalmamasını sağlamak.

> ⚠️ **Bu görev A1'i (URL biriktirme) ÇÖZMEZ.** Dış denetimde ortaya çıktı:
> pencere `GET /t` anından başlıyor, yani "fiziksel dokunuştan beri geçen süreyi"
> değil "sayfayı açtıktan sonra butona basana kadar geçen süreyi" ölçüyor.
> Saldırgan telefonu **uçak moduna** alıp plakete 10 kez dokunur — çip her
> okumada `ctr`'ı artırır, URL adres çubuğunda görünür, ama istek sunucuya hiç
> ulaşmaz. Ertesi gün evden URL'yi açar → `GET` *o an* kaydedilir → 5 saniye
> sonra POST → pencere içinde → geçer.
> A1'in gerçek karşılığı **`tap:ctrGap` + `base:ctr-gap-review`** politikasıdır
> (Q21) ve tam çözüm değildir — ADR 0005'te kabul edilen risktir.

**Neden yine de gerekli.** [M5-04](m5-tap-akisi.md) bilinçli olarak sayacı sayfa
açılışında değil POST anında ilerletir (sayfayı açıp basmayan kullanıcı sayacı
harcamasın). Bu doğru UX kararının bedeli: açılmış bir sayfa süresiz geçerli
kalır. Pencere bu bedeli sınırlar — plakete dokunup sayfayı açık bırakan,
saatler sonra (belki başka bir yerden) basan kullanıcıyı durdurur.

**Tasarım.**
1. `GET /t` anında `(tag_uid, ctr, first_seen_at, source_ip)` kaydedilir.
   *(Alan adı bilerek `request_fingerprint` değil: `scripts/redline-check.sh` R1
   `fingerprint` desenini FAIL olarak tarar ve §4.1'in dili tarayıcı parmak izine
   kapalıdır. Tutulan şey yalnız kaynak IP, yalnız anomali metriği için.)*
   Bu tablo **kendi migration'ıyla gelir**: `tap_page_views(tag_uid, ctr,
   first_seen_at, source_ip)` + `tenant_id` + RLS beşlisi + saklama süresi
   (eski satırlar temizlenir — kimliksiz herkes `GET /t` açabildiği için
   temizliksiz tablo ucuz bir disk doldurma vektörüdür).
2. `POST /api/checkin` yalnız `first_seen_at + pencere` içindeyse kabul edilir.
3. Pencere **sınırlı parametre**: guardrail kapatılamaz, süre tenant tarafından
   **1–15 dk** arası seçilir (varsayılan 3 dk).
4. Kayıt: pencere aşımı `sys:tap-freshness` ile `deny` → **kayıt yine yazılır**
   (§4.6), verdict `reject`, gerekçe açık.

**Kabul kriterleri.**
- Pencere içinde normal tap etkilenmiyor (kullanıcı butona basana kadar geçen
  birkaç saniye).
- Pencere dışı POST → `reject` + gerekçe `sys:tap-freshness`.
- **Sıra senaryosu korunuyor:** A dokunur, arkasından B dokunur, A butona basar
  → A hâlâ geçerli. (Reddedilen alternatif: `GET`'te `seen_ctr` ilerletmek —
  daha sıkı ama arkadaki kişinin dokunuşu öndekinin sayfasını geçersizleştirir
  ve §5'in "ardışık farklı kişiler hepsi `ok`" gereksinimini kırar.)
- Çevrimdışı kuyruk (M9-01) ile uyumlu: kuyruğa alınan istek `queued` damgası ve
  `base:queued-window` politikasıyla ayrı değerlendirilir — **ama üst sınır
  `sys:occurred-at-bound` guardrail'indedir**, kapatılamaz.
- **Satır yalnız CMAC doğrulandıktan sonra yazılır.** Aksi hâlde saldırgan
  `GET /t?tag=<UID>&ctr=<gelecek değerler>&cmac=<çöp>` ile gelecek sayaçlar için
  eski `first_seen_at` damgalı satırlar yaratır; ertesi gün o plakete dokunan
  meşru kullanıcıların POST'u "pencere dışında" bulunup **reject** alır. UID gizli
  değildir (URL'de açık). Aynı sebeple POST **en yeni** GET kaydına bakar.
- Sayaç boşluğu (`tap:ctrGap`) hesaplanıp kayda yazılıyor ve
  `base:ctr-gap-review` politikasına besleniyor (Q21). M2-06'daki "> 1000 → flag"
  eşiği kaldırıldı: biriktirme ~10'luk boşluk üretir, 1000 onu hiç görmez.
- Tespit sinyalleri M6-11'de: **POST'suz `GET /t` sayısı** *ve* — asıl önemlisi —
  **`ctr` boşlukları**. İlki uçak modu senaryosunda sıfır kalır, ikincisi kalmaz.

**Tuzaklar.**
- Pencereyi çok kısa tutmak (< 1 dk) meşru kullanıcıyı düşürür: telefon kilidi
  açılır, sayfa yüklenir, kullanıcı ekranı okur. 3 dk makul başlangıç.
- `first_seen_at` sunucu saatidir; istemciden gelen zamana **asla** güvenilmez.
- Bu görevi "A1 kapandı" diye işaretleme. Kapanmadı; sinyal ekledi.
