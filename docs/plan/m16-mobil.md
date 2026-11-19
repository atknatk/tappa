# M16 — Personel mobil uygulaması (isteğe bağlı) + NFC'siz telefonlar

> **Durum:** plan **onaylandı** — yedi bağımsız denetim turundan sonra ONAY (`407710bf`, 2026-11-13). **ADR 0045 kabul
> edildi (2026-11-14, M16-01).** `main`'e birleştirilmedi. Kod yok. Canlı durum yalnız [state.md](state.md)'dedir;
> bu dosyaya "yapıldı" yazılmaz.
>
> **Karar belgesi:** [ADR 0045](../adr/0045-istege-bagli-native-uygulama.md) (durum *Kabul edildi (2026-11-14)*).
> Kartlar ADR'nin sözlüğünü kullanır.
>
> **Atıf kuralı:** kırmızı çizgiler hep **"CLAUDE.md §4.x"**; `file:line` atıfları `origin/main` `70e8a016`'ya göredir
> ve yapıcı kendi turunda yeniden okur. **Doğrula** = planı yazan doğrulayamadı; kart ölçer.
>
> **Test adı kuralı:** `TestEveryNamedTestExists` reponun bütün metin dosyalarında geçen her `Test…` adının var
> olmasını ister. Bu belge yalnız **var olan** testleri ve `TestDecide_` gibi var olan önekleri adlandırır; yeni testler
> **tarif edilir**, adlarını yapıcı koyar. (Bu yüzden Apple'ın beta dağıtım kanalı aşağıda adıyla değil *"App Store
> Connect iç testi"* diye geçer — adı o desene uyar.)
>
> **Sevk:** her faz kendi dalında biter; `main`'e birleştirme bir **deploy kararıdır** ve kullanıcınındır (CLAUDE.md
> §10). Faz A sunucu yüzeyi uygulamasız da zararsızdır (yeni uç noktalar, yeni sütun NULL) ve ayrı sevk edilebilir.
> Mağazaya yükleme (Faz F) ayrı bir kullanıcı kararıdır.
>
> **Sıra (kullanıcı kararı, 2026-11-14):** Faz 0'dan sonra **A → B + C paralel** (ayrı worktree'ler), ardından
> **D, E, F**. **2026-11-18 (kullanıcı kararı):** Faz F'den (yayın) **önce** Faz G (personel self-servis, M16-30 …
> M16-40) gelir — sıra **D, E, G, F**. Faz G'nin kartları numarada F'den sonra, dosyada ve uygulamada F'den öncedir.

## Kullanıcı kararları (2026-11-12/13) — bağlayıcı

Yalnız bu beş madde kullanıcının **açık** kararıdır; yeniden tartışılmaz (ADR 0045 §2'de *"karar"* diye işaretli):

- Native uygulama **isteğe bağlı**; tarayıcı akışı kalır ve tam desteklenir.
- **Tamamen native:** iOS Swift/SwiftUI, Android Kotlin/Jetpack Compose — Node içeren yol (React Native / Expo)
  dışarıda (CLAUDE.md §1).
- NFC'siz telefon için **statik QR ve canlı QR birlikte**.
- NFC'siz aktivasyon: lokasyonda **canlı QR ekranı varsa onunla, yoksa müdür onayıyla**.
- **Attestation** Faz E'de **yalnız kanıt + rapor**; güveni ve verdict'i değiştirmez.

## Kullanıcı kararları (2026-11-18, Faz G) — bağlayıcı

Ana oturumun AskUserQuestion'ıyla alındı; yeniden tartışılmaz:

1. **Kapsam ve sıra:** dört özelliğin dördü de mağaza yayınından **önce** — sıra **1 → 3 → 2 → 4**: Saatlerim →
   e-postayla giriş → ayarlar/gizlilik → App Review hazırlığı (kartlar M16-30 … M16-41; uygulama sırası Faz G başlığında — M16-30 ayar kartı 2026-11-18'in ikinci kararı, M16-41
   denetim turunun gizlilik kartı).
2. **Giriş = *"sign in with your work email"*.** Çalışan iş e-postasını yazar, e-postayla **tek kullanımlık** bir
   kod/link alır; cihaz bağı yine **işverenin plaketine ilk gerçek NFC dokunuşunda** tamamlanır (CLAUDE.md §5
   *"Aktivasyon dokunuşu"*, ADR 0026). **Kendi kendine kayıt YOK:** hesabı işveren açar. Yalnız **kayıtlı e-postası olan
   aktif** çalışanlar. Oran sınırlı; **hesap numaralandırması yok** (adres kayıtlı olsa da olmasa da aynı cevap).
   Güvenlik sınırını değiştirdiği için ADR ister (bugün aktivasyonu yalnız müdürün bastığı davet başlatır) →
   [ADR 0049](../adr/0049-calisanin-e-postayla-girisi.md) (*Önerildi*). **Ana oturum kararı (2026-11-18, denetim H3):**
   uç **varsayılan kapalıdır** — env `TAPPA_EMAIL_SIGNIN_TENANTS` (boş = kapalı) ve önce yalnız demo işletme, kullanıcının
   onayladığı ayrı bir config PR'ıyla (M16-35).
3. **App Review yolu = ayrı bir demo işletme (*"Taptime Demo"*) + inceleme notunda bir davet + basılabilir statik QR**
   — inceleyici uygulamayı plaketsiz kullanabilsin. Faz D'nin M16-22'sine (uygulamada QR okuma) bağlıdır. QR dokunuşu
   `flag` olur (`base:qr-requires-ip`) ve bu kabul — inceleyici gerçek FLAGGED kaşesini görür. **Uygulama içi sahte demo
   modu YOK.** ⚠️ Plan yazılırken ölçülen boşluk: statik QR **aktivasyonu tamamlamaz** (CLAUDE.md §5: aktivasyon yalnız
   gerçek NFC'de; M16-07 kart düzeltmesi *"yalnız NFC"*; NFC'siz yollar M16-23'te) — inceleyicinin aktivasyonu
   [Q47](open-questions.md)'dedir — cevap (a) müdür onayı, (c) canlı QR yedek (2026-11-19).

4. **Saatlerim'i işletme yönetir** (2026-11-18, aynı günün ikinci kararı): owner panelden *"show My hours to staff"*
   aç/kapa ve bir **görünürlük penceresi** seçer (kullanıcının örneği *"1 hafta"*); bazı işverenler hiç istemez.
   **Sunucu zorlar**, yalnız arayüz değil: kapalıyken imzalı uç ve tarayıcı sayfası reddeder, pencere sorguda uygulanır;
   uygulama sekmeyi `/app/v1/me`'den öğrenir, kapalıyken hiç göstermez. Varsayılan **kapalıdır** ([Q48](open-questions.md) (a), 2026-11-19). Demo
   işletmede açıktır. → M16-30 (yeni kart; sonraki kartlar bir kaydı). Pencere denetim turunda **bütün haftalara** çevrildi
   (1 · 2 · 4 · 13 hafta; gerekçe M16-30).

5. **Faz G'nin açık soruları cevaplandı** (2026-11-19, AskUserQuestion — her soruda önerilen seçenek): **Q44** (a) giriş
   linki **24 saat** · **Q45** (b) e-postayla giriş **isteği yalnız uygulamada** (tarayıcı isteği Faz G dışı ayrı kart;
   tamamlanma tarayıcıda da olur) · **Q46** (b) silme talebi **panel + owner e-postası** · **Q47** (a) inceleyici **müdür
   onayıyla** aktive olur, (c) canlı QR notta yedek · **Q48** (a) Saatlerim **herkes için kapalı** doğar · **Q49** (a) e-postayla
   giriş **owner ayarı, varsayılan kapalı** · **T59 tersine çevirmesi** (a) **evet** — `tap.signed_out_phone` satırı ve anomali
   listesi (2), yalnız listedeki ve girişi açık işletmelerde, yalnız tarayıcı yolunda. Kartlar seçilen dala daraltıldı;
   reddedilenler tek satırlık *"reddedildi"* notudur.

**Gerekçe — Apple App Store Review Guidelines** (ana oturumun 2026-11-18 okuması; numaralar o okumadan, bu belgeyi yazan
yeniden açmadı — **doğrula** M16-40'ta):

- **5.1.1(v):** kayıt zorunlu tutulamaz; *"If your app supports account creation, you must also offer account deletion
  within the app."* → M16-37/M16-38 (uygulama içi hesap silme talebi).
- **2.1(a):** *"include demo account info (and turn on your back-end service!) if your app includes a login"*; demo
  modu yalnız *"with prior approval by Apple"*; gereken donanım/kaynak gönderilir (Apple'ın kendi örneği: *"a sample QR
  code"*). Asıl ret riski budur: inceleyicinin daveti ve NTAG plaketi yoktur → M16-39.
- **4.2:** uygulama yeniden paketlenmiş bir web sitesinden fazlası olmalı → Saatlerim (M16-30 … M16-33) bunu güçlendirir.
- **3.1.3 / 3.1.3(c) Enterprise Services:** işletmenin kaydı ve faturası uygulamada değil, web'de kalır; uygulama
  yalnız personel içindir, hesapları işveren açar.
- **4.8:** Sign in with Apple gerekmez (kendi hesap sistemimiz; işletme hesabı istisnası).

## ADR kabulüyle kesinleşen kararlar (2026-11-14)

> **Kullanıcı 2026-11-14'te ADR 0045'ü olduğu gibi kabul etti; aşağıdakiler o kabulle karardır** (ADR 0045 *"Kabul
> notu"*). Önceden *"plan önerisi"*ydiler: kullanıcı 2026-11-12'de planı gördü ve yazılmasını onayladı, ayrıca karar
> vermemişti. Önerilen **adlar** (ör. `base:mock-location-review`, `/app/v1` uç noktaları) kartlarda yine yapıcının
> ölçümüyle kesinleşir.

- Aynı plaket, aynı URL; uygulama kuruluysa Universal Link / App Link onu açar.
- Sunucu otoriter: aynı `tap.Decide` + policy motoru; çalışan tap ekranı kuralları (§9) uygulamada da geçerli.
- Kod yeri `mobile/ios/`, `mobile/android/`; Flutter / KMP reddi; üçüncü parti SDK yok (her bağımlılık zaten
  CLAUDE.md §1 gereği sorulur).
- Kazanımlar: iOS Core NFC ön plan okuma (P6) · dışa aktarılamayan cihaz anahtarı + imzalı istek + tek kullanımlık
  nonce (Q11, oturum kopyalama) · sahte konum bildirimi → tenant'ın değiştirebildiği baseline · `transactions.client`.
- Canlı QR'ın biçimi: Go'nun sunduğu ekran sayfası, panelden lokasyona eşlenir, ~20 sn'lik imzalı token; varsayılan
  `qr_live` + (IP veya GPS) → `ok`, yoksa `flag`; lokasyon başına isteğe bağlı; kişi başına debounce geçerli.
- Müdür onayının biçimi: bekleyen cihaz listesi, eşleşme kodu, `audit_log`.
- NFC'siz yol için reddedilenler: BLE beacon, Wi-Fi SSID/BSSID, paylaşılan kiosk + PIN.
- Çevrimdışı kuyruk sonraya; anlamı: kuyruktaki dokunuş sunucu saatini alır, eşik ötesi `flag`.
- Faz sırası (0 · A · B/C · D · E · F · Sonra).

## Brief iddialarının doğrulaması (2026-11-13, `70e8a016`)

| İddia | Kodda | Sonuç |
|---|---|---|
| Plaket URL'si sabit, uygulama aynı URL'yi alabilir | `internal/handler/tap.go:282` (`TapPath`), `:290` (`PlaqueTapBase`) | ✅ — plaket yeniden kodlanmaz |
| Statik QR = SUN'suz `/t` | `internal/sun/params.go:188-192` | ✅ `ctr` ve `cmac` yoksa `ChannelQR` |
| Kanal sözlüğü kapalı | `00005:114`, `00043:141`, `00046:137`, `00048:171`, `00067:193`, `00095:66` | ⚠️ **10 migration + 2 sorgu + Go'da çok sayıda karşılaştırma** (liste bir başlangıçtır; kanıt M16-21'in parite testi) — `qr_live` geniş bir değişiklik (ADR 0045 Bedel 6) |
| Akış `qr_live`'ı görür mü | `00067:193`, `checkin.go:1687` (`feedEligible`), `openapi.yaml:1105` | ❌ bugün **sessizce atlar**; `/v1` enum'u kapalı → Q38 |
| Canlı QR varsayılanı yeni baseline ister mi | `baseline.go:287-309` | ⚠️ **istemeyebilir:** yalnız `base:qr-requires-ip` kanal koşullu (`channel == "qr"`) |
| Tazelik ve SUN guardrail'leri | `guardrails.go:396`, `:479` | NFC-only → `qr_live` için karar gerekir (M16-21) |
| Debounce'un sunucu saati bacağı | `db/queries/transactions.sql:317` (`SecondsSinceLastRecordedTap`) | ❌ `channel IN ('nfc', 'qr')` → `qr_live` girmezse geriye tarihlenmiş bir `qr_live` dokunuşu debounce'u **sessizce** atlar (M16-19, M16-21) |
| Motor her tap'te plaket varsayar | `internal/domain/tap/types.go:471-473` (`Tag` *"Always present for a resolved tap"*), `/v1` `tagId` (`openapi.yaml:1130-1133`) | ❌ `qr_live` satırının plaketi yok → M16-21 |
| Tenant bilinmeden çözümleme | `00003` `resolve_session_by_token_hash` (`tappa_resolver`, SECURITY DEFINER, sabit `search_path`) | cihaz, ekran oturumu ve eşleşme kodu aynı desenle; dokunuş anındaki ekran `00004` `resolve_tag_by_uid` deseniyle çözülür (M16-03, M16-19) |
| Yeni baseline belgesi var olan tenant'a nasıl girer | `checkin/policyset.go:422`, `:529` | ilk dokunuşta **onaysız** materialize edilir (ADR 0045 Bedel 10) |
| `transactions` INSERT yetkisi | ADR 0038 | **sütun listesi** → `client` için ayrı GRANT |
| İkinci cihaz ilkini iptal eder | `activate.go:1068-1077` | ✅ → uygulamaya genişler (Q40) |
| Panelin *"telefonlarını çıkar"* eylemi | `employeeactions.go:690` (T59) | ✅ var → uygulama cihazlarını da kapsamalı (M16-09) |
| Oturum paketi attestation yapmaz | `internal/session/manager.go:18-22` | ✅ → cihaz doğrulaması **ayrı pakette** |
| Repoda Android var mı | `android/` (ADR 0017 encode rölesi, `mt.taptime.relay`) | ⚠️ **başka uygulama**; yerinde kalır, toolchain ölçüsü oradan alınır (`android/README.md`) |
| Kırmızı çizgi taraması mobil kodu görür mü | `scripts/redline-check.sh:33` (`SRC`), `:2530` (`B1_SRC`) | ❌ `mobile/` yok → M16-02 kod kartlarından **önce** |
| CI mobil derler mi | `.github/workflows/*.yml` | ❌ hayır → Q36 (d) |
| Son migration / ADR | `db/migrations/00099_*`, `docs/adr/0043-*` | ✅ `70e8a016`'da sıradaki 00100, ADR 0044 idi; `origin/main` birleşmesinden (`ca8b94d4`, 2026-11-14) sonra son migration **00103**, son ADR **0044** (e-posta değiştirme, T173) → M16'nın ADR'si **0045**, migration'ları **00104**'ten; ikinci `origin/main` birleşmesinden (`d28014d6`, 2026-11-18) sonra son migration **00107** (OAuth, issue #37 — canlıda), son ADR **0047** → M16 migration'ları **00108**'den |

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı** bir denetçi kabul
  kriterlerini tek tek doğrular; bulgu → düzeltme → **yeni** denetçi. **"Mercek: iki"** = ayrıca agent
  `tappa-security-auditor` (şema, karar motoru, güvenlik sınırı). **"Mercek: tek"** = yalnız üçüncü göz.
- **Büyük testler yalnız FAZ SONUNDA** (kullanıcı kuralı, 2026-10-08/09: *"her adımda test yapıp çok zaman
  kaybetme"*). Tur içinde yalnız değişen testler `-run` ile + saniyelik kapılar (`gofmt`, build, `bash
  scripts/redline-check.sh`). Tam `-race`, `go test ./...`, `make check`, mobil test paketlerinin tamamı ve gerçek
  cihaz turu **faz kapanış kartında bir kez** (M16-10, M16-14, M16-18, M16-24, M16-26, M16-40, M16-29). Bu cümle her yapıcı
  ve denetçi brief'ine yazılır.
- **Her kart kendi commit'inde yeşildir** — bir kartın kırdığı test o kartta düzeltilir.
- **`make gen`** her `.templ` / `db/queries/*.sql` değişikliğinden sonra.
- **Paralel hatlar:** Faz A bitince B (Android) ve C (iOS) ayrı worktree'lerde paralel yürür; ortak dosya yalnız
  `mobile/README.md` ve bu kart dosyasıdır (birleştirmede çakışma beklenir, içerik çakışması beklenmez).
- **Dal:** feature dalı; `main`'e birleştirme kullanıcının deploy kararı.

## Her kartın miras aldığı kısıtlar

- **Sunucu karar verir.** Uygulama SUN doğrulamaz, yön hesaplamaz, verdict göstermeden önce tahmin etmez; ekrandaki
  her metin sunucudan gelir ya da tarayıcı ekranının birebir eşidir.
- **Kanal ve `client` sunucudan türetilir** (ADR 0004 §8); gövde, başlık, user-agent okunmaz. Sahte konum bir istemci
  **beyanıdır**: yalnız sıkılaştırır.
- **Uygulama yolu `Record`'a iner** — tarayıcı yolunun kullandığı aynı servis, aynı kişi kilidi (ADR 0006), aynı
  `transactions` INSERT'i. İkinci bir yazma yolu açılmaz.
- **§4.1:** biyometri API'si yok, anahtar erişim denetiminde biyometri/kullanıcı varlığı bayrağı yok.
- **§4.2:** yalnız tap anında tek seferlik konum; arka plan izni, arka plan modu, konum servisi, geofence yok.
- **§4.7 + §7:** uygulamada plaket anahtarı yok; log'da token, imza, nonce, tam koordinat, davet kodu, e-posta yok;
  mağaza imza anahtarları repoda yok (depo PUBLIC).
- **Yeni sütunlar** yalnız INSERT, tarihsel satırda NULL, geri doldurma yok; `transactions` ALTER'ları
  `SET LOCAL lock_timeout = '1s'` ile başlar, CHECK'ler `NOT VALID` + ayrı `VALIDATE` (`00044` deseni); `Down` dolu
  satırda reddeder.
- **Yeni bağımlılık önce sorulur** (CLAUDE.md §1) — Q36/Q37 cevaplanmadan ilgili kart başlamaz.

## Migration ve ADR numaraları

İlk birleşmede `main`'in son migration'ı **00103**, son ADR'si **0044** (`docs/adr/0044-panel-kimliginin-e-posta-degisikligi.md`,
T173; `0040` hiç kullanılmadı) — `origin/main` `c89c3b5b` birleşmesinden (`ca8b94d4`, 2026-11-14) sonra. Bu yüzden
M16'nın ADR'si **0045**'tir (yeniden numaralandı; planın ilk sürümleri onu 0044 diye anar). Aşağıdaki dağıtım **öneridir**;
her kart açılırken `main`'in o anki son numarası **yeniden okunur** ve numara oradan alınır (başka dallar araya
girebilir; goose, uygulanmış en yüksek sürümün altındaki bir dosyayı sonradan *missing* sayar — M15'in 2026-10-22
numara değişikliği notu).

**Yeniden numaralandırma (2026-11-18):** ikinci `origin/main` birleşmesi (`d28014d6`) `main`'in OAuth migration'larını
(00104–00107, issue #37, ADR 0047) getirdi; bunlar canlıda uygulanmış olduğundan numaraları değişmez. M16'nın hiçbir
ortamda uygulanmamış üç dosyası içerik ve sıra korunarak **00104 → 00108**, **00105 → 00109**, **00106 → 00110**
oldu; tablodaki sonraki öneriler de dört kaydı. ADR'ler: `main` 0046 (timesheets) ve 0047'yi (OAuth) kullandı, M16'nınki
**0045** kalır.

| No (öneri) | Kart | İçerik | `Down` |
|---|---|---|---|
| 00108 | M16-03 | `app_devices` + `app_request_nonces` (§6 beşlisi, GRANT) + cihaz çözümleyicisi (SECURITY DEFINER, `tappa_resolver` — `00003` deseni) | satır varsa reddeder |
| 00109 | M16-04 | `transactions.client` + değer kümesi CHECK'i `client IS NULL OR client IN ('browser','app')` (NULL'u kabul eder — `00065` deseni) + manuel satır CHECK'i `channel <> 'manual' OR client IS NULL`, ikisi de `NOT VALID`; tap kanalında `client`'ı zorunlu kılan CHECK **yok** + sütun INSERT GRANT'ı (ADR 0038) | dolu değer varsa reddeder |
| 00110 | M16-04 | iki CHECK'in `VALIDATE`'i (`00044` deseni; NULL'lu tarihsel satırlar geçer) | `NOT VALID` hâline döner |

| 00111 | M16-19 | `live_qr_displays` (+ eşleşme kodu) + üç çözümleyici: ekran oturumu ve eşleşme kodu (`00003` deseni), dokunuş anındaki ekran → tenant/lokasyon/iptal (`00004` `resolve_tag_by_uid` deseni) | satır varsa reddeder |
| 00112 | M16-19 | `qr_live` kanal değeri — bütün kanal CHECK'leri ve akış tetikleyicisi (envanter M16-19'da) | `qr_live` satırı varsa reddeder |
| 00113 | M16-23 | müdür onaylı aktivasyon: bekleyen cihaz isteği (§6 beşlisi, GRANT) | satır varsa reddeder |
| 00114 | M16-25 | attestation sonucu (sütun ya da tablo — kart seçer; tabloysa §6 beşlisi, GRANT) | dolu değer varsa reddeder |
| 00115 | M16-30 | `tenants.staff_hours_weeks smallint` + `CHECK (… IS NULL OR … IN (1, 2, 4, 13))` + `GRANT UPDATE (staff_hours_weeks)`; varsayılansız (Q48 (a), 2026-11-19 — her satır NULL = kapalı) — `00064`'ün `day_cutoff` deseni | `DROP COLUMN` (yapılandırma; geçmişi `audit_log`'da — `00064:181-182`) |
| 00116 | M16-35 | `employee_invites.origin text NOT NULL DEFAULT 'employer'` + `CHECK (origin IN ('employer','self'))`; çözümleyici `resolve_employees_for_signin(citext, uuid[])` (SECURITY DEFINER, `tappa_resolver`, `OPERATOR(public.=)` — `00011:295-313`) + **`GRANT SELECT (id, tenant_id, email, status) ON employees TO tappa_resolver`** + **`GRANT SELECT (suspended_at, email_signin) ON tenants TO tappa_resolver`** (yalnız yeni sütunlar — `tenants.id` `00051:155`'ten beri var, dokunulmaz); `tenants.email_signin boolean NOT NULL DEFAULT false` + `GRANT UPDATE (email_signin)` (Q49 (a)) | `origin='self'` satırı varsa reddeder; yoksa `DROP FUNCTION`, `REVOKE SELECT (id, tenant_id, email, status) ON employees FROM tappa_resolver`, `REVOKE SELECT (suspended_at, email_signin) ON tenants FROM tappa_resolver`, `DROP COLUMN`'lar |
| 00117 | M16-35 | `CREATE INDEX CONCURRENTLY employees_email_idx ON employees (email) WHERE email IS NOT NULL` — **ayrı dosya**, `-- +goose NO TRANSACTION`, kendi `lock_timeout`'u (CLAUDE.md §6 sıcak tablo kuralı; `00091` emsali) | `DROP INDEX CONCURRENTLY` |

**Sıra kısıtı (M16-04 denetimi, 2026-11-14):** M16-03 (00108) `main`'e M16-04'ten (00109/00110) **önce ya da onunla
birlikte** ulaşır. 109/110'a çıkmış kalıcı bir veritabanı (`tappa-m16-04-db` dahil) 00108'i sonradan **alamaz** — depo
goose'u `-allow-missing` ile koşmaz — ve yeniden kurulmalıdır.

**Faz G numara ölçümü (2026-11-18, `origin/main` `6ab423eb`):** son migration **00110**, son ADR **0047**; bu planın
00111–00114 ayırması yukarıda; **ADR 0048** `origin/ops/tappa-scoped-access` dalında kullanılıyor
(`docs/adr/0048-kapsamli-insan-erisimi.md`, `main`'de yok — state.md *"ADR 0048 `scoped-access.sql`"*) → Faz G'nin ADR'si
**0049**, migration'ları **00115**'ten. Hesap silme talebi tablosu (eski Q46 (c)) denetim turunda Faz G'den çıkarıldı —
numara ayrılmaz. M16-31 migration beklemez: kişinin kayıtlarını okuyacak indeks
`transactions_tenant_employee_occurred_idx` (`00005:175`, `(tenant_id, employee_id, occurred_at DESC)`) zaten var — kart
planı ölçer. Kural aynı: her kart açılırken `main`'in ve diğer dalların son numarası **yeniden okunur**.

ADR: **0045** bu milestone'un karar belgesi. Attestation'ı karar verici yapmak ya da çevrimdışı kuyruk **ayrı ADR**dir.
Faz G'nin e-postayla girişi **ayrı ADR**dir: [0049](../adr/0049-calisanin-e-postayla-girisi.md) (*Önerildi*, M16-34'te
kabul edilir).

## Kullanıcı tarafı ön koşulları

Bunlar ajanın yapamayacağı işlerdir; ilgili kart bunlar olmadan başlamaz.

| Ön koşul | Neden | İlk bloklandığı kart |
|---|---|---|
| **Apple Developer Program — kurum hesabı** — ✅ **hazır** (kullanıcı, 2026-11-14); yayıncı **Everva Bilişim**. **Team ID kullanıcıdan alınacak.** | Team ID → `apple-app-site-association`; Associated Domains, NFC, App Attest yetkileri; App Store Connect | M16-08 (Team ID), M16-15 |
| **Google Play Console** hesabı — ✅ **hazır** (kullanıcı, 2026-11-14); yayıncı **Everva Bilişim** | paket adı, Play App Signing sertifika parmak izi → `assetlinks.json`; Play Integrity; iç test kanalı | M16-08 (parmak izi), M16-11 |
| **Uygulama adı ve paket/bundle kimliği** (Q35) — açık; yayıncı hesap Everva Bilişim | mağaza adı kullanılabilirliği; geri alınamaz | M16-08, M16-11, M16-15 |
| **Gerçek cihazlar** (açık — hazır diye bildirilmedi): iPhone XS veya sonrası · iPhone 7–X arası bir telefon · NFC'li bir Android · NFC'siz bir telefon · canlı QR ekranı için bir tablet/eski telefon | Universal Link, Core NFC, Keystore/Secure Enclave, sahte konum ve canlı QR yalnız gerçek cihazda ölçülür | M16-14, M16-18, M16-24 |
| **M8-07 telefon envanteri** (KF/KM çalışanlarının telefon modelleri) — açık, hazır diye bildirilmedi | P6'nın gerçek oranı; pilotta kimin uygulamaya ihtiyacı olduğu | M16-29 |
| Güncel **Xcode** kurulu bir Mac (bu makinede ölçülmedi — **doğrula**) | iOS derlemesi | M16-15 |
| Mağaza imza anahtarlarının **saklama yeri** (öneri: Play App Signing + yükleme anahtarı ve Apple sertifikaları Infisical'da) | §4.7'nin ruhu, depo PUBLIC | M16-28 |
| Hukuk metni onayı (`/legal/privacy` değişikliği, mağaza gizlilik etiketleri) | GDPR Art. 13; mağaza beyanı yasal beyandır | M16-27 |
| **Demo işletme *"Taptime Demo"*** — kullanıcı (ya da onun gözetiminde ana oturum) **ürünün kendi yüzeylerinden** açar: `/signup` ya da operatör; üretimde seed / `make db-reset` **asla**. İçeriği M16-39'da: bir lokasyon, **iki** demo çalışan (adresleri panel girişinden farklı), bir manager girişi. **Zamanı:** M16-35'in config PR'ından önce (PR demo UUID'sini taşır) — M16-34 kabul edildikten sonra | App Review 2.1(a); e-postayla girişin ilk ve tek açık işletmesi | M16-35 (config PR), M16-39 |
| **Demo plaketi** — stoktan **bir** plaket, encode aracıyla kodlanır (`00025`; atama `encoded_at` ister — `db/queries/tags.sql:426`) ve demo lokasyonuna atanır; kalıcı olarak demo işletmenindir, `tags` satırı asla silinmez | statik QR plaket UID'si ister (`internal/sun/mint.go:81`) | M16-39 |
| **E-postayla girişin canlıya çıkışı** — M16-35'in ayrı config PR'ı (`TAPPA_EMAIL_SIGNIN_TENANTS` = demo UUID); birleştirme deploy'dur, **kullanıcı onaylar** | denetim H3 | M16-40 |
| **Canlı taban ölçümü** (salt okuma: adresli personel, KF/KM'de çift adres, davet hacmi, aktivasyon başarısızlığı, K-22 durumu) — kullanıcı ya da ana oturum | ADR 0049'un sayıları | M16-34 |
| **Gizlilik bildiriminin yayımı** — hukukçu onayı + operatörden yayım | M16-38'in linki; mağaza gönderimi | M16-41 |
| **App Store Connect inceleme notu ve Play inceleme erişimi** — davet linki ve (Q47 (a)) manager panel kimliği **yalnız** App Store Connect / Play Console'a yazılır, depoya asla (depo PUBLIC) | 2.1(a) | M16-39 |
| Faz G'nin §9 kararları (panelin Saatlerim ayarı, e-postayla giriş ayarı ve panel sinyalleri, uygulama ana ekranı, Saatlerim'in durum görünümü, e-postayla giriş girişi) — AskUserQuestion + mockup | CLAUDE.md §9 | M16-30, M16-32, M16-33, M16-35, M16-36 |

## Kart listesi

| Faz | Kart | Başlık | Mercek |
|---|---|---|---|
| 0 | M16-01 | ADR 0045'ün kabulü ve CLAUDE.md / handoff metinleri | iki |
| A | M16-02 | Kırmızı çizgi taraması `mobile/`'ı görür | iki |
| A | M16-03 | Şema: `app_devices` ve tek kullanımlık nonce | iki |
| A | M16-04 | Şema: `transactions.client` | iki |
| A | M16-05 | `internal/appdevice`: imzalı istek doğrulaması | iki |
| A | M16-06 | Policy bağlamı: `Client`, `MockLocation`; sahte konum baseline'ı | iki |
| A | M16-07 | `/app/v1` JSON yüzeyi: activate / tap / me / revoke | iki |
| A | M16-08 | `/.well-known` dosyaları (Universal Link / App Link) | tek |
| A | M16-09 | İptal yolları uygulama cihazlarını da kapatır | iki |
| A | M16-10 | Faz A kapanışı | iki |
| B | M16-11 | Android iskeleti (`mobile/android/`) | tek |
| B | M16-12 | Android cihaz anahtarı ve aktivasyon | iki |
| B | M16-13 | Android tap ekranı: NFC, tek seferlik konum, sahte konum | iki |
| B | M16-14 | Faz B kapanışı — gerçek cihaz turu | iki |
| C | M16-15 | iOS iskeleti (`mobile/ios/`) | tek |
| C | M16-16 | iOS cihaz anahtarı ve aktivasyon | iki |
| C | M16-17 | iOS tap ekranı: Universal Link + Core NFC ön plan okuma | iki |
| C | M16-18 | Faz C kapanışı — gerçek cihaz turu | iki |
| D | M16-19 | Şema: canlı QR ekranları ve `qr_live` kanal envanteri | iki |
| D | M16-20 | Canlı QR ekran sayfası ve panelden eşleme | iki |
| D | M16-21 | `qr_live` karar motorunda | iki |
| D | M16-22 | Uygulamada QR okuma (statik + canlı) | tek |
| D | M16-23 | NFC'siz aktivasyon: canlı QR ve müdür onayı | iki |
| D | M16-24 | Faz D kapanışı | iki |
| E | M16-25 | Attestation — yalnız kanıt | iki |
| E | M16-26 | Raporlar ve panelde cihaz listesi; Faz E kapanışı | iki |
| G | M16-30 | Saatlerim ayarı: işletmenin aç/kapa ve görünürlük penceresi (şema + panel) | iki |
| G | M16-31 | Saatlerim: kişinin kendi kayıtlarının okuması + `POST /app/v1/hours` (imzalı okuma) (M9-05'i karşılar) | iki |
| G | M16-32 | Saatlerim: tarayıcı sayfası (oturum çerezli çalışan) | iki |
| G | M16-33 | Uygulama ana ekranı ve Saatlerim — iOS + Android (§9 onayı önce) | iki |
| G | M16-34 | ADR 0049'un kabulü ve canlı taban ölçümü (sorular 2026-11-19'da cevaplandı) | iki |
| G | M16-35 | E-postayla giriş: sunucu (istek ucu, çözümleyici, sınırlar, numaralandırmasızlık) | iki |
| G | M16-36 | E-postayla giriş: uygulama ekranları | iki |
| G | M16-37 | Hesabı silme talebi: sunucu ve işverene iletim | iki |
| G | M16-41 | Gizlilik bildirimi: uygulama paragrafı ve yayım (**M16-38'den önce**) | tek |
| G | M16-38 | Ayarlar ve gizlilik ekranı — iOS + Android | tek |
| G | M16-39 | App Review hazırlığı: inceleme notu, basılabilir QR, inceleyicinin aktivasyonu | tek |
| G | M16-40 | Faz G kapanışı | iki |
| F | M16-27 | Gizlilik: `/legal/privacy`, mağaza etiketleri | tek |
| F | M16-28 | İmza, iç test dağıtımı, sürüm süreci | iki |
| F | M16-29 | KF/KM pilotu ve M16 kapanışı | tek |

---

# Faz 0 — Karar

## M16-01 — ADR 0045'ün kabulü ve CLAUDE.md / handoff metinleri

- **Bağımlılık:** —
- **Kırmızı çizgi:** §4.1, §4.2, §4.5, §4.7 (ADR'nin analizi), §1 (stack), §5, §9
- **Araç:** ana oturum; kullanıcı onayı · **Mercek: iki** (ADR bir güvenlik sınırı tanımlar: taşıyıcı tokensız kimlik)
- **Commit:** `docs(adr): accept the optional native staff app`

**Amaç.** ADR 0045'ü *Kabul edildi*'ye geçirmek; Q35–Q42'yi sormak (cevap ya da hangi kartı blokladığı yazılır);
ürün ve stack cümlelerini kabul edilen karara eşitlemek.

**Bu kartta değişen cümleler** (satırlar `70e8a016`'ya göre; yapıcı yeniden okur):

| Dosya | Bugün | Olacak |
|---|---|---|
| `CLAUDE.md:6` | *"tarayıcı açılır (uygulama kurulumu YOK)"* | *"tarayıcı açılır (uygulama kurulumu **gerekmez**; isteğe bağlı native uygulama aynı URL'yi açar — ADR 0045)"* |
| `CLAUDE.md` §1 tablosu (`:15-24`) | mobil satırı yok | iki satır: *Mobil (isteğe bağlı)* — iOS Swift/SwiftUI, Android Kotlin/Compose, `mobile/`, Go modülü dışında, üçüncü parti SDK yok (ADR 0045); ve mevcut `android/` rölesinin ADR 0017 açılımı |
| `CLAUDE.md:26-27` | *"Node/npm/node_modules bu repoda ASLA yer almaz"* | **değişmez** — yanına: *"mobil derlemeler de Node'suzdur (RN/Expo bu yüzden reddedildi, ADR 0045)"* |
| `CLAUDE.md` §3 dizin haritası (`:50-150`) | `android/` ve `mobile/` yok | `android/` (encode rölesi, ADR 0017) · `mobile/ios/` · `mobile/android/` · `internal/appdevice/` (cihaz anahtarı doğrulaması — kart M16-05 yazınca) |
| `CLAUDE.md:158-159` (§4.1) | *"Telefonun kendi ekran kilidi bizim katmanımız değildir"* | aynen + *"uygulamada biyometri API'si ve biyometrik anahtar erişim denetimi yoktur (ADR 0045 §3)"* |
| `CLAUDE.md:160-161` (§4.2) | *"Arka plan konum, sürekli takip, geofence izleme YOK"* | aynen + *"uygulamada arka plan konum izni ve modu da yoktur"* |
| `CLAUDE.md` §3 `internal/sun/` satırı (`:76-77`) | *"Kriptografi SADECE burada"* | *"**Plaket** kriptografisi (AES-CMAC, SUN, `ctr`) SADECE burada"* — `internal/appdevice`'in ECDSA doğrulaması (M16-05) ve canlı QR token imzası (M16-20) bu cümleyle çelişmesin; yeni iki yer §3'e kendi satırıyla girer (`internal/appdevice/` M16-05, canlı QR paketi M16-20) |
| `CLAUDE.md` §7 *"Asla loglanmaz"* listesi (`:474-` paragrafı) | oturum token'ı, CMAC, AES anahtarı, davet kodu, e-posta, tam GPS; operatör ve dış API tarafı | listeye eklenir: **cihaz imzası, istek nonce'u, canlı QR token'ı, ekran eşleşme kodu, ekran oturumu çerezi, canlı QR imza anahtarı**, müdür onayı eşleşme kodu (hata mesajı ve `audit_log` `detail`'i dahil) |
| `CLAUDE.md` §11 tablosu (`:589-600`) | mobil satırı yok | *"iOS / Android personel uygulaması — `mobile/README.md` + ADR 0045"* |
| `docs/handoff.md:24` | *"tarayıcı otomatik açılır (uygulama kurulumu YOK)"* | *"… (uygulama kurulumu gerekmez; isteğe bağlı uygulama — ADR 0045)"* |
| `docs/handoff.md:27` slogan · `:54-55` Connecteam karşılaştırması · `:60` *"app-less akış"* farkı | — | **Q42'nin cevabına göre**; cevap *"kalsın"* ise değişmez |
| `docs/plan/roadmap.md` *"MVP kapsamı dışında"* | *"mobil uygulama"* | *"mobil uygulama (→ M16, isteğe bağlı)"* |

**§5 ve §9'un normatif cümleleri bu kartta yazılmaz**, onları uygulayan kartla aynı commit'te yazılır (M15-01'in
deseni: CLAUDE.md §5 *"kod bundan sapıyorsa kod yanlıştır"* der; kodu olmayan bir §5 cümlesi o anda kodu yanlış ilan
eder):

| CLAUDE.md paragrafı | Kart |
|---|---|
| §5 *"Dört kanıt"* — *"oturum çerezi ya da uygulama cihaz anahtarı = kim"* | M16-07 |
| §5 karar tablosu satır 6 notu + yeni baseline `base:mock-location-review` | M16-06 |
| §5 *"QR kanalı"* — canlı QR (`qr_live`) cümleleri | M16-21 |
| §5 *"Aktivasyon dokunuşu"* — uygulama, canlı QR ve müdür onayı yolları | M16-12 / M16-16 (uygulama), M16-23 (NFC'siz) |
| §9 tap ekranı — uygulama ekranlarının eşliği; eski iPhone'un tek düğmesi (Q41) | M16-13, M16-17 |
| §3 `internal/appdevice/` | M16-05 |

**Kabul kriterleri.**
- ADR 0045 durum satırı *Kabul edildi (TARİH)*; Q35–Q42 her biri ya *cevaplandı (TARİH)* ya da *"açık — bloklar
  M16-xx"*.
- Yukarıdaki ilk tablonun her satırı uygulandı; ikinci tablonun satırları bu kartta **uygulanmadı** (diff'te §5/§9
  değişikliği yok).
- `go test -run 'TestEveryNamedTestExists|TestClaudeMD_TheIdentityExceptionIsADR0031sSection61' ./cmd/tappa/` yeşil
  (CLAUDE.md'ye dokunan kart CLAUDE.md'yi okuyan testi koşar).
- `state.md` M16 satırı: *"ADR 0045 kabul edildi, Faz A açık"*.

---

# Faz A — Sunucu temeli

## M16-02 — Kırmızı çizgi taraması `mobile/`'ı görür

- **Bağımlılık:** M16-01
- **Kırmızı çizgi:** §4.1, §4.2, §4.7, §7 (log)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(audit): scan the mobile apps for the red lines`

**Amaç.** `mobile/` dizini **boşken** taramayı kurmak: ilk uygulama satırı yazıldığında kapı zaten kapalı olsun.

**Kabul kriterleri.**
- `scripts/redline-check.sh` `SRC`'ye `mobile` eklendi (dizin yoksa tarama hatası değil, *"yok"* raporu — mevcut
  eksik yol davranışı ölçülüp ona uyulur).
- R1 tetikleyicilerine platform kalıpları: `LocalAuthentication`, `LAContext`, `BiometricPrompt`, `androidx.biometric`,
  `USE_BIOMETRIC`, `USE_FINGERPRINT`, `biometryAny`, `biometryCurrentSet`, `userPresence`, `NSFaceIDUsageDescription`,
  `setUserAuthenticationRequired`; passkey API'leri (platform kimlik doğrulayıcısı biyometriyi açar):
  `ASAuthorizationPlatformPublicKeyCredentialProvider`, `androidx.credentials`, `CreatePublicKeyCredentialRequest`.
  (Mevcut `face[_-]?id` / `touch[_-]?id` / `webauthn` zaten bir kısmını yakalar — ölç, tekrarlama.)
- R2'ye: `ACCESS_BACKGROUND_LOCATION`, `foregroundServiceType="location"` (ve `FOREGROUND_SERVICE_LOCATION`),
  `requestLocationUpdates`, `GeofencingClient`, `requestAlwaysAuthorization`, `allowsBackgroundLocationUpdates`,
  `startMonitoringSignificantLocationChanges`, `startMonitoring(for:`, `CLCircularRegion`, `UIBackgroundModes` içinde
  `location`; iOS'un sürekli/arka plan yolları: `startUpdatingLocation`, `startMonitoringVisits`, `CLMonitor`,
  `CLLocationUpdate` / `liveUpdates`, `CLBackgroundActivitySession`; `Info.plist` anahtarları
  `NSLocationAlwaysAndWhenInUseUsageDescription`, `NSLocationAlwaysUsageDescription`. (`startUpdatingLocation` tek
  seferlik okuma için de kullanılabilir; uygulama `requestLocation` kullanır — kural bunu bilerek FAIL sayar, M16-17
  ona uyar.)
- B1 (`Tappa` kullanıcı metninde) `B1_SRC`'ye `mobile/*/` kullanıcı metni dosyaları eklendi (Android `res/values`,
  iOS `*.strings` / `Localizable.xcstrings`, Swift/Kotlin dize literal'leri — kapsam ölçülerek seçilir).
- R7 log kalıpları Swift (`os_log`, `Logger`, `print`) ve Kotlin (`Log.`) çağrılarını da görür: token, imza, nonce,
  canlı QR token'ı, eşleşme kodu, koordinat, e-posta, davet kodu (CLAUDE.md §7'nin M16-01'de genişleyen listesi).
- Her yeni kalıp için `cmd/tappa/scripttest`'te mevcut redline testlerinin deseninde (ör. `redline_b1_test.go`) bir
  pozitif ve bir negatif fikstür; fikstür `testdata` altında, gerçek `mobile/`'a dosya konmaz.
- Mevcut `android/` rölesi bu kartın kapsamı **dışında** kalır mı içinde mi — ölçülür, gerekçesiyle yazılır
  (`android/README.md` *"redline'ın SRC listesi onu taramaz"* der).

> **Kart düzeltmesi (2026-11-14, M16-02 uygulaması sırasında).** *"Fikstür `testdata` altında"* maddesi, kartın
> örnek gösterdiği desenle (`redline_b1_test.go`) çelişiyordu ve o desen izlendi: fikstürler test dosyasındaki Go
> literal'leridir, her koşuda `t.TempDir()` altındaki geçici bir ağaca yazılır (`redlineRun`). Gerekçe ölçülebilir:
> `cmd/` `SRC`'nin içindedir ve `testdata/`'yı yalnız B1 dışlar — `cmd/tappa/scripttest/testdata/` altına konan bir
> `LocalAuthentication` satırı gerçek depoyu R1'de kırmızıya çevirirdi. Aynı sebeple test dosyasının kendi metni de
> ihlali yazmaz: her fikstür `~~` ile maskelidir ve koşuda açılır (`redline_api3_test.go`'nun kuralı). Gerçek
> `mobile/`'a dosya konmadı. Testler: `cmd/tappa/scripttest/redline_m16_test.go`.

**Tuzaklar.** Kalıplar sözcükseldir: `startMonitoring(for:` gibi bir yazım Swift'in başka API'sini de yakalayabilir —
muafiyet kuralı (R1'in `R1_WAIVER_PHRASES` deseni) gerekçesiz eklenmez.

> **Denetim turu (2026-11-14, M16-02 iki mercek).** Kapatılanlar mekanizmayla: **S1** (uygulama kaynağı `android/` ve
> `mobile/` dışında → FAIL; `mobile/`'ın *isimle* isteğe bağlı olması tek başına bir delikti) · R7'nin konum nesnesi,
> adsız `code` ve QR değeri için **ifade kökleri** (yalnız `mobile/` — rölenin `location=${p.location}` satırı HTTP
> başlığıdır) ve `.format(` / `String(format:` içine giren pencere · Apple'ın `Logger.<kategori>.info(` adeti · R1
> ön-süzgeci `mobile/` ve `android/` altında uygulanmaz · R2: Tuist/JSON sözlük dizisi, yorumlu plist dizisi ve
> **aynı dosyada tek seferlik konum + tekrarlayan zamanlayıcı** ağı. Ucuz ve 0 isabet ölçülen LOW eklemeler de girdi.
> Aşağıdakiler **adıyla sınırdır**, kapatılmadı (betiğin sınır notlarında da yazılı):
> - Periyodik ağ dosya bazlıdır: zamanlayıcı başka dosyadaysa, `Handler.postDelayed` kendini yeniden kuruyorsa,
>   `Task { while !Task.isCancelled` ya da Combine `.timer(` kullanılıyorsa görünmez. Gerçek kapı M16-13/M16-17'nin
>   çağrı-yeri izin listesi testidir.
> - R7: ara değişken (`let t = token; print(t)`) · interpolasyonda ikinci seviye çağrı (`\(String(describing: token))`)
>   · `Timber.tag(TAG).d(..)` · başka adlı logger örneği · çıplak `url` (API taban adresini loglamak olağan; NDEF/SUN
>   URL'si `sunUrl`/`ndefUrl` adıyla taşınır) · `mobile/`'da HTTP `location` başlığı da konum kökü sayılır (uygulama
>   başlık değişkenine başka ad verir).
> - R2 kaynak metne bakar; derlenmiş Info.plist'i M16-15 ölçer (aşağıdaki kabul kriteri).
> - **M16-11'e — bilinen yanlış pozitif:** bir kütüphanenin iznini söken
>   `<uses-permission android:name="…ACCESS_BACKGROUND_LOCATION" tools:node="remove"/>` koruyucu bir satırdır ama R2'de
>   FAIL verir. Bugün muafiyet **eklenmedi** (satır yok); gerekirse M16-11 onu **yola ve satıra bağlı**, gerekçeli bir
>   muafiyetle yazar (R7d'nin sha256'lı satır muafiyeti deseni) — toplu bir `tools:node` süzgeci değil.
> - R1/R2/R7 `mobile/**/*.md`'yi de tarar (muafiyet yok): `mobile/README.md` yasak API'yi birebir adıyla yazarsa FAIL.
>
> **İkinci denetim turu (2026-11-14) — kartın son sertleştirme turu.** Kapatılanlar: konum dizisinin elemanı
> (`locations.last!`, `.first`, `.last()`) ve `"%s".format(location)` · `code` kökü yalnız **çıplak** (`${response.code}`,
> `\(error.code)` FAIL değil) · argüman başlı kök (`, x` / `+ x` / ilk argüman) yalnız argüman sınırında biter
> (`"…, fix acquired: $hasFix"` FAIL değil) · S1'in muafiyeti `android/`'in tamamı değil **röle modülü** (`android/app/` +
> iki kök Gradle betiği) ve R2 `android/` altındaki her konum iznini FAIL eder (öncül artık mekanik) · S1 `.java` ve
> `.gradle` · `rg --no-ignore-dot` (`.ignore`/`.rgignore` taramadan düşüremez) · XcodeGen tırnaklı liste
> (`- "location"`) · Kotlin `while (isActive)`. Zorla eklenmiş ignore'lu dosyalar için ayrı `-ci` çağrısı **gerekmedi**:
> `--cached` onları zaten listeler (ölçüldü). **Kalan, adıyla sınır:**
> - Tuist `.array([.string("location")])` biçimi R2'ye görünmez — derlenmiş Info.plist denetimi (M16-15) kapatır.
> - Bilinen yanlış pozitifler, **M16-11/M16-13 yeniden ölçer:** `mobile/`'da `location` adlı bir HTTP başlığı ·
>   argüman olarak geçen çıplak `fix`/`loc`/`code`/`qr…` değeri (ör. bir sayaç için `code`) · dize içinde `$code`.
> - Bilinen yanlış negatifler: alan yolu üzerinden gelen bir davet/eşleşme kodu (`${invite.code}`) — kod adını taşıyan
>   değişkenler (`inviteCode`, `pairingCode`) tetikleyicilerle yakalanır; uygulama çıplak `code` alanı yerine adlı
>   değişken kullanır.

## M16-03 — Şema: `app_devices` ve tek kullanımlık nonce

- **Bağımlılık:** M16-01
- **Kırmızı çizgi:** §4.4 (nonce tüketimi tek ifade), §4.5, §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add app devices and single-use request nonces`

**Amaç.** Uygulama cihazının açık anahtarını ve isteğe bağlı nonce'ları taşıyan iki tablo.

**Kabul kriterleri.**
- `app_devices`: `tenant_id`, `employee_id`, platform (`ios`|`android`), açık anahtar (ham bayt ya da SPKI — kart
  seçer, gerekçesiyle), anahtar parmak izi (tekil), kaba cihaz etiketi (nullable, *"iPhone"* düzeyinde —
  `sessions.device_info`'nun sözü), `created_at`, `last_used_at`, `revoked_at` (**tek yönlü**: `00099`'un
  `sessions` deseni).
- `app_request_nonces`: `tenant_id`, `device_id`, nonce'un **özeti** (nonce'un kendisi saklanmaz), `expires_at`,
  `consumed_at`. Tüketim tek ifade: `UPDATE … SET consumed_at = now() WHERE … AND consumed_at IS NULL AND expires_at >
  now() RETURNING …`.
- İki tabloda §6 beşlisi + GRANT'lar en dar (sütun düzeyi UPDATE: `last_used_at`, `revoked_at`, `consumed_at`).
- RLS izolasyon testi (A tenant'ı B'nin cihazını görmez) — CLAUDE.md §6'nın *"filtre taşımayan"* şekliyle.
- **Tenant bilinmeden çözümleme.** İmzalı bir istek geldiğinde tenant henüz bilinmez; FORCE RLS altında `tappa_app`
  cihaz satırını göremez. Cihaz kimliği → (tenant, çalışan, açık anahtar, iptal durumu) çözümlemesi **dar bir SECURITY
  DEFINER fonksiyonudur**, `00003`'ün `resolve_session_by_token_hash` deseninde: sahibi `tappa_resolver`, en çok 1
  satır döner, `tappa_resolver`'a yalnız gereken sütunlarda SELECT, `SET search_path = pg_catalog, pg_temp`, EXECUTE
  yalnız `tappa_app`'e. Nonce tüketimi tenant çözüldükten **sonra**, `SET LOCAL app.tenant_id` altında normal sorguyla
  yapılır. Testler: RLS testi (fonksiyonsuz `tappa_app` başka tenant'ın cihazını göremez) + definer yüzeyi envanteri
  (`TestResolverAccess_NoSqlcQueryNamesADefiner`, `TestResolverAccess_TheCallSitesAreTheOnesInventoried` yeni
  çağrıyla güncellenir).
- **Eşzamanlılık testi:** aynı nonce ile N goroutine → **tam 1** başarı, `-race`.
- Süresi dolmuş nonce'ların temizliği: kart karar verir (silme mi, bırakma mı); karar §4.3'e değmez (bu tablo
  `transactions` değil) ama `redline-check` R3 kapsamında mı ölçülür.

> **Kart düzeltmesi (2026-11-14, M16-03 uygulaması sırasında).** İki madde metinden saptı, ikisi de ölçümle:
> (1) *"anahtar parmak izi"* sütunu `key_digest` adını aldı — İngilizce karşılığı `redline-check` R1'in biyometri
> tetikleyicisidir (ölçüldü: ilk taslak 11 satırla R1 FAIL verdi); kural kör tutulur, ad değişir (deponun her seferki
> yönü). Sütun `public_key`'in hex SHA-256'sı, `GENERATED ALWAYS … STORED`, global UNIQUE. (2) Açık anahtar **ham
> sıkıştırılmamış P-256 noktası** (65 bayt, `0x04‖X‖Y`) — SPKI değil: tek kanonik bayt dizisi, böylece özet ve UNIQUE
> anahtarın kendisine aittir. Ek kararlar: cihaz satırı aktivasyonun **sonunda, etkin** doğar (bağ davette durur,
> ADR 0026 deseni — *"bağlı ama etkin olmayan cihaz"* bu tablonun durumu değildir); nonce ömrü CHECK ile ≤ 5 dk;
> ölü nonce'lar (tüketilmiş/süresi dolmuş) **silinir**, cihaz başına, yeni nonce verilmeden önce
> (`DeleteDeadAppRequestNonces`) — silme fail-closed'dur (tüketim canlı satır ister) ve R3 kapsamı dışında kalır
> (R3 yalnız `transactions`/`punch_events`'i tarar; bu tablo kanıt taşımaz). `consumed_at` da tek yönlüdür (tetikleyici).

## M16-04 — Şema: `transactions.client`

- **Bağımlılık:** M16-01
- **Kırmızı çizgi:** §4.3, §4.6, §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): record which client wrote a tap`

**Amaç.** Her yeni tap satırının hangi istemciden geldiğini değişmez olarak yazmak.

**Kabul kriterleri.**
- `transactions.client text NULL`; **değer kümesine bir** CHECK `client IS NULL OR client IN ('browser','app')` (NULL'u
  kabul eder) + **manuel satırın client taşımadığına bir** CHECK `channel <> 'manual' OR client IS NULL` (güvenlik
  denetimi, 2026-11-14 — `clientColumn`'un manuel kuralının DB yarısı; eski ikili her yerde NULL yazdığı ve tarihsel
  satırlar NULL olduğu için rollout'ta güvenli), ikisi de `NOT VALID`; tarihsel satırlar NULL, geri doldurma yok,
  `DEFAULT` yok.
- **"Tap kanalında `client` zorunlu" CHECK'i YOK.** `NOT VALID` bir CHECK yine her yeni INSERT'i bağlar: migration ile
  yeni pod'un yayılması arasındaki pencerede eski ikili `client` yazmaz ve her NFC/QR dokunuşu 23514 ile reddedilip
  **kaybolurdu** (§4.6); `VALIDATE`'i de geçmişi olan her veritabanında NULL'lu tarihsel satırlarda düşerdi. M15 aynı
  şekli bu yüzden reddetti — `00065:22-29` (*"a slip that reached a CHECK would refuse the INSERT with 23514 and lose
  the record"*, *"Historical rows are NULL and pass … NOT VALID still binds every new row"*) ve `Down`'un *"the CHECKs
  admit NULL"* gerekçesi (`00065:57`). Yeni satırların dolu olduğunun kanıtı **kod tarafındadır**: Go testleri
  (tarayıcı yolu `browser`, uygulama yolu `app`, manuel yol NULL) + M16-10'un uçtan uca DB testi.
- `tappa_app`'in INSERT sütun listesine `client` eklendi (ADR 0038 — tablo düzeyi INSERT geri gelmez; ölç:
  `has_column_privilege`).
- `InsertTransaction` ve bütün `store.Transaction` döndüren sorgular güncellendi (`make gen`); tarayıcı yolu
  `browser` yazar (`internal/domain/checkin` — sunucudan türetilmiş, istek alanı değil).
- `VALIDATE` ayrı migration'da (`00044` deseni); `Down`'lar dolu değerde reddeder.
- Holding projeksiyonu (`00046`) ve `/v1` akışı `client`'ı **taşımaz** (bu kartta) — taşıyacaksa ayrı karar.

## M16-05 — `internal/appdevice`: imzalı istek doğrulaması

- **Bağımlılık:** M16-03
- **Kırmızı çizgi:** §4.1, §4.4, §4.7, §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(appdevice): verify signed app requests`

**Amaç.** Uygulama isteğinin kimliğini (cihaz → çalışan, tenant) taşıyıcı token olmadan çözmek.

**Kabul kriterleri.**
- Yalnız stdlib (`crypto/ecdsa`, `crypto/elliptic` ya da `crypto/ecdh`'nin uygun yolu, `crypto/sha256`); yeni
  bağımlılık yok.
- Kanonik metin tek fonksiyonda tanımlıdır (yöntem, yol, gövde özeti, nonce, cihaz kimliği — ADR 0045 §2.3) ve iki
  platformun uygulayacağı **bilinen cevap vektörleri** `test/fixtures` altında yayımlanır (sahte anahtarlarla; §4.7).
- Çözümleme oturumun gerçeklerini döndürür, **karar vermez** (`internal/session/manager.go:8-16`'nın kuralı): iptal
  edilmiş cihaz ayrı bir sonuçtur ve çağıran onu **§5 satır 3'ün ikizi** olarak işler (401 / yeniden aktivasyon,
  **kayıt yok**); deaktive bir çalışanın cihazı **iptal edilmemiştir** ve çözülür — satır 4'ü `employees.status` +
  `sys:employee-deactivated` üretir. İki durum birbirine çevrilmez: çevrilirse ya bir kayıt kaybolur ya da bir
  kayıtsız yol kayıt yazar (§4.6).
- Tablo bazlı testler: geçerli imza · yanlış anahtar · bozuk imza · tekrar kullanılan nonce · süresi dolmuş nonce ·
  başka cihazın nonce'u · başka tenant'ın cihazı · iptal edilmiş cihaz.
- Paket hiçbir log satırına imza, nonce, açık anahtar ya da gövde yazmaz (log testi; CLAUDE.md §7 listesi M16-01'de
  genişler).
- CLAUDE.md §3 `internal/sun/` satırının *"Kriptografi SADECE burada"* ifadesi M16-01'de *"plaket kriptografisi"*ye
  daraltılmış olmalı; değilse bu kart onu düzeltir.
- CLAUDE.md §3'e `internal/appdevice/` satırı (M16-01 tablosu).
- **M16-03 incelemesinden devralınan (2026-11-14):**
  - HTTP katmanı **bilinmeyen** ve **iptal edilmiş** cihazı imza doğrulamasından **önce** aynı yanıtla karşılar
    (durum kâhini yok); iptal ile bilinmeyen ayrımı yalnız sunucu içinde, kayıtsız yolun seçiminde kullanılır.
  - Nonce **sunucuda** üretilir: 32 bayt `crypto/rand`; istemciden gelen bir nonce asla kabul edilmez.
    Saklanan, özetidir (`nonce_hash`, `00108`).
  - Açık anahtar Go'da **eğri üstünde** doğrulanır (`ecdsa.ParseUncompressedPublicKey`); `00108`'in CHECK'i yalnız
    uzunluğu (65) ve önek baytını (`0x04`) sabitler, eğri aritmetiğini yapamaz.
  - İptal, cihazın nonce'larını aynı ifadede siler ve tüketim cihazı `FOR SHARE` ile yeniden okur (`00108`,
    `RevokeAppDevice`/`ConsumeAppRequestNonce`); Go tarafındaki iptal denetimi yine yapılır, ama tek savunma değildir.
    ⚠️ **`EXISTS … FOR SHARE` kemeri yük taşır, gereksiz değildir:** *"issue uçuşta, sonra revoke"* sırasında bir
    nonce iptalin DELETE'inden kurtulur (ifade anlık görüntüsü issue'nun commit'inden eskidir — M16-03 güvenlik
    yeniden denetimi ölçtü) ve onu yalnız kemer reddeder. Kaldırılmaz.
  - **Kilit sırası kuralları** (M16-03 güvenlik yeniden denetimi deadlock'ları ölçtü): **D1** — aynı transaction'da
    `DeleteDeadAppRequestNonces` sonra `IssueAppRequestNonce`, eşzamanlı bir iptalle deadlock verdi ve kurban
    **iptal** oldu (çalınan bir telefon iptali tekrar tekrar düşürebilirdi); **D2** — iki istek, her biri önce
    tüketip sonra `TouchAppDevice`, kilit yükseltmesiyle 40P01 verdi. Kurallar: önce **cihaz satırı**, sonra
    **nonce satırları**; temizlik **nonce verildikten sonra** ya da **kendi transaction'ında**; `TouchAppDevice`
    **tüketimden önce**; iptali çağıran 40P01'de **yeniden dener**.

> **Uygulama notu (2026-11-14, M16-05 uygulaması sırasında).** Kartın bıraktığı biçim kararları, ölçümle:
> - **Kanonik metin** altı satır, her biri tek LF ile biter: `tappa-app-request-v1` (alan ayırma öneki) · yöntem
>   (büyük harf) · yol (kablodaki kaçışlı yol; **sorgu dizesi reddedilir**, imzalanmaz — uygulamanın yolları sabit,
>   veri gövdede) · ham gövde baytlarının küçük harf hex SHA-256'sı · nonce (verildiği base64url dizesi) · cihaz
>   kimliği (yalnız kanonik küçük harf biçim). Tek tanım `internal/appdevice/canonical.go` `canonical`; vektörler
>   `test/fixtures/appdevice_vectors.json` (metinler printf ile yazıldı, imzalar **OpenSSL** ile üretilip doğrulandı —
>   Go kendi çıktısını doğrulamıyor).
> - **İmza** ECDSA P-256 / SHA-256, ASN.1 DER (iki platformun ürettiği biçim), başlıkta katı dolgusuz base64url.
>   Başlıklar: `App-Device-Id`, `App-Nonce`, `App-Signature` (her biri tam bir kez). **Yüksek S kabul edilir**: Secure
>   Enclave ve Keystore düşük S'ye normalize etmez; tekrar koruması nonce'tur, imzanın tekliği değil
>   (`TestVerify_HighSTwinOfASpentRequestIsRefused`).
> - **Nonce** 32 bayt `crypto/rand`, kabloda 43 karakter katı base64url; `nonce_hash` ham baytların hex SHA-256'sı.
>   Ömür `NonceTTL` = 60 sn. Ölü nonce süpürmesi verişten **sonra**, aynı transaction'da (D1).
> - **Retler:** bilinmeyen, iptal edilmiş, kötü imza ve harcanmış/yabancı/süresi dolmuş nonce **aynı metni** basar
>   (`appdevice: request not authenticated`) ve aynı durumu (401) alır; ayrım yalnız `errors.Is` ve `Reason()` ile,
>   sunucu içinde. İptal edilmiş cihaz imzadan **önce** reddedilir ve `Device` **boş** döner (oturumun `ErrRevoked`'ından
>   farklı: cihaz kimliği kimlik bilgisi değildir, imza doğrulanmadan hiçbir şey atfedilmez; deaktivasyon iptal etmediği
>   için satır 4'e bu yoldan hiç gidilmez). Veritabanı arızası 401 değil 500'dür.
> - **Sayılan sınır, M16-07'ye:** nonce isteği imzalanamaz (imzanın ihtiyacı odur), bu yüzden `IssueNonce` cihaz
>   kimliğini bilen herkese *"bu kimlik canlı mı"* sorusunu cevaplar. Kimlik yalnız TLS üstünden giden rastgele bir
>   UUID'dir; yetmezse handler canlı olmayan kimliğe saklamadığı bir **yem nonce** dönebilir. Cihaz başına canlı nonce
>   üst sınırı (M16-07) `IssueNonce`'un transaction'ına, cihaz satırını önce ayrı bir ifadeyle kilitleyerek girer.
> - **Ölçülen sızıntı, kapatıldı:** `*struct` / `*[32]byte` alanlı bir değer, çağıranın dışa kapalı alanında `%s`/`%d` ile
>   basılınca fmt'nin kötü-fiil yolu hedefi derinlik 0'da açıp imza ve nonce baytlarını onluk yazdı. `SignedRequest`
>   artık `**signedRequest`, `Nonce` `*string` taşır (`TestSignedRequest_NoLeakOnAnyRenderingPath`).

## M16-06 — Policy bağlamı: `Client`, `MockLocation`; sahte konum baseline'ı

- **Bağımlılık:** M16-04
- **Kırmızı çizgi:** §5 (normatif), §4.6, CLAUDE.md §8 (%90+)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(policy): review a tap whose phone reports a mock location`

**Amaç.** `tap.Input`'a `Client` ve `MockLocation`; bağlam anahtarları (öneri: `tap:client`, `tap:mockLocation` —
mevcut `tap:*` adlandırması, `internal/domain/tap/decide.go:93`); yeni baseline `base:mock-location-review` (ADR 0045
§2.4 — ad M16-01'de kesinleşir).

**Kabul kriterleri.**
- Anahtarlar `policy_context`'e yazılır (P5 gereği simülatör görür); `validate.go`'nun kapalı anahtar sözlüğüne girer.
- Baseline belgesi `review`, kaynak `location/*` (`tapScope`); `BaselineVersion` artar ve `BaselineChanges` bir
  duyuru taşır — `TestBaseline_ContentIsPinnedToItsStamp` yeşil.
- `TestDecide_` önekli yeni vakalar: sahte konum + IP → `flag` · sahte konum + GPS-only → `flag` · `MockLocation`
  yok/false → bugünkü sonuçlar **aynen** (tarayıcı yolu için regresyon) · `Client` sonucu tek başına değiştirmez.
- Güven puanı formülü değişmez (test).
- Tenant kuralı ekranı yeni belgeyi gösterir (`tenant.Rulebook`; *"Not set up yet"* durumu dahil).
- CLAUDE.md §5: satır 6 altına sahte konum cümlesi + baseline adı (bu kartla).

**Tuzaklar.** Yeni belge var olan tenant'lara **ilk dokunuşta onaysız** girer (`checkin/policyset.go:422`, `:529`;
ADR 0045 Bedel 10). Uygulamasız tenant'ta sonuç değişmediği ölçülmeli: `MockLocation` yalnız `/app/v1` yolunda dolar.

## M16-07 — `/app/v1` JSON yüzeyi: activate / tap / me / revoke

- **Bağımlılık:** M16-05, M16-06
- **Kırmızı çizgi:** §4.4, §4.5, §4.6, §5, §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(app): add the signed app surface`

**Amaç.** Uygulamanın konuştuğu tek yüzey; tarayıcı akışının **eşi**, ikinci bir karar yolu değil.

**Kabul kriterleri.**
- Uç noktalar (öneri adları; kart kesinleştirir): **aktivasyon iki adımdır** — `POST /app/v1/activate/bind` (davet
  kodu + onay + açık anahtar: ADR 0026'nın onay POST'unun eşi — onayı kaydeder ve bu **anahtara** bağlar, daveti
  tüketmez, cihazı etkinleştirmez) ve `POST /app/v1/activate/complete` (bağlı anahtarla imzalı; gövdede ilk gerçek NFC
  okumasının SUN URL sorgusu — Faz D'de canlı QR token'ı, M16-23), `POST /app/v1/nonce`, `POST /app/v1/tap/start` (SUN URL'sinin sorgusu → `GET /t`'nin
  eşi: önizleme, tahmini yön, **sayaç ilerlemez**, imzalı tap bağlamı), `POST /app/v1/tap` (`POST /api/checkin`'in
  eşi: bağlam + `want` + konum + sahte konum bayrağı), `GET /app/v1/me` (ad, tenant markası, tahmini yön — eski
  iPhone'un tek ekranı için, Q41), `POST /app/v1/revoke` (cihazın kendi çıkışı).
- Durum değiştiren her istek imzalı + tek kullanımlık nonce (M16-05) — **iki istisnayla, açıkça:** `activate/bind`
  henüz bir cihaz satırı yokken gelir (nonce `device_id`'ye bağlıdır, M16-03), tekrarına karşı korumayı davetin onay +
  bağ kuralı verir (ikinci bind ilkini taşır, ADR 0026 *"Güvenlik değerlendirmesi"*); `activate/complete` bağlı ama
  etkin olmayan cihazdan gelir ve **nonce almaz** — tekrar koruması SUN'un atomik `ctr`'ıdır (aynı okuma ikinci kez
  `sun.Verify`'dan geçemez, §4.4) ve tek ifadeli tüketimdir; imza bağlı anahtarla. Etkin olmayan cihaza nonce
  verilmez. İmzasız istek 401, kayıt yazmaz — **ama**
  kimliği çözülmüş bir istek §5'in kayıt yazan satırlarına (4, 6, 7) tarayıcıyla aynı şekilde iner.
- Aynı `checkin.Service.Record`; `client='app'` sunucudan; tap bağlamı `GET /t`'ninkiyle aynı imzalı yapı (tazelik
  guardrail'i aynı saati okur).
- **Aktivasyonun tamamlanması `CompleteByTap`'in sırasıdır** (ADR 0026 md. 3): davet geçerli → kanal NFC →
  ilerletmeyen ön kontrol (plaket davetle **aynı tenant'ta**, `active`) → **`sun.Verify`** (önce CMAC, sonra atomik
  `ctr`, CLAUDE.md §4.4) → onay + bağ **tek ifadenin** WHERE'inde tüketim → cihaz etkin. `transactions` satırı yazmaz;
  ikinci cihaz kuralı Q40'ın cevabına göre. Eşzamanlılık testi: aynı `(tag, ctr)` ile N eşzamanlı tamamlama → **tam 1**
  başarı, `-race`.
- **Okuma isteklerinin kimliği.** `GET /app/v1/me` ve `POST /app/v1/tap/start` ad ve tenant markası döndürür; ikisi de
  imzalıdır — durum değiştirmedikleri için nonce yerine imzalı bir zaman damgası (sunucu saatine göre dar pencere,
  ör. ±60 sn; kart ölçer) ve cihaz kimliği taşırlar; imzasız ya da pencere dışı → 401, gövde boş. `POST
  /app/v1/nonce` imzalı + zaman damgalıdır ve **cihaz başına oran sınırlıdır** (yaşayan tüketilmemiş nonce sayısına
  da üst sınır — tablo şişirilemesin).
- Holding dokunuşu (KM çalışanı KF plaketinde, ADR 0027) tarayıcıdaki sonucun aynısını verir (DB testi).
- Oran sınırı: tap yüzeyinin limiter'ı (`internal/httpx/ratelimit.go`) cihaz başına uygulanır; yanıtlar `no-store`.
- Hata gövdesi kapalı sözlük; hiçbir yanıt ve log satırı imza, nonce, tam koordinat, davet kodu taşımaz.
- CLAUDE.md §5 *"Dört kanıt"* cümlesi (bu kartla, M16-01 tablosu).
- **M16-03 incelemesinden devralınan (2026-11-14):**
  - `CreateAppDevice` yalnız **anahtara sahiplik kanıtından sonra** çağrılır (sunucunun verdiği meydan okuma
    üzerinde imza): `key_digest` global UNIQUE olduğundan, kanıtsız bir kayıt başkasının açık anahtarını önceden
    kaydedip onun aktivasyonunu kilitleyebilirdi.
  - `app_devices_key_digest_key` ihlali (23505) kapalı sözlükte açık bir *"yeni anahtar üret"* yanıtına eşlenir;
    uygulama her aktivasyonda **yeni bir anahtar çifti** üretir.
  - Cihaz başına yaşayan nonce üst sınırı **atomik** olmalı: aynı transaction'da **önce cihaz satırını ayrı bir
    ifadeyle kilitle** (`SELECT … FOR NO KEY UPDATE`) **ya da cihaz başına advisory kilit al**, sonra say ve ver.
    `CountLiveAppRequestNonces` sonra `IssueAppRequestNonce` (say-sonra-yaz) eşzamanlı isteklerde sınırı aşar — `ctr`
    dersinin aynısı (§4.4). Sayımı `INSERT … SELECT … WHERE (SELECT count(*) …) < N` içine katlamak **atomik
    değildir**: M16-03 güvenlik yeniden denetimi `FOR SHARE OF d` ile de `FOR UPDATE OF d` ile de N=1'de iki eşzamanlı
    verişin ikisinin de yazıldığını ölçtü.
  - **Kilit sırası kuralları** (M16-05'teki D1/D2 ölçümleri): önce cihaz satırı, sonra nonce satırları; ölü nonce
    temizliği verişten **sonra** ya da **kendi transaction'ında** (D1'de kurban iptaldi); `TouchAppDevice`
    tüketimden **önce** (D2, 40P01); iptal çağıranı 40P01'de yeniden dener. `EXISTS … FOR SHARE` kemeri yük taşır,
    kaldırılmaz.
  - **M16-05 incelemesinden devralınan (2026-11-14):**
    - **Nonce üst sınırı:** `IssueNonce` kaynak başına hız sınırlıdır; sınır aşılınca **en eski canlı nonce düşürülür,
      istek asla reddedilmez** — reddetmek, cihaz kimliğini bilen herkesin gerçek telefonu kilitlemesine izin verirdi.
      Sınır, cihaz satırı **önceki bir ifadede** kilitliyken koşar (yukarıdaki madde).
    - **Yem nonce seçilirse:** canlı olmayan kimliğe yem nonce dönülecekse bilinmeyen/iptal edilmiş dallar da sabit bir
      anahtarla sahte bir `ParsePublicKey` + `VerifyASN1` koşar; yoksa ~0,2 ms'lik zaman farkı onları yine ayırır.
    - **Yönlendirme:** `FromHTTP`'den önce `r.URL.Path`/`RawPath`'i yeniden yazan ara katman yok (`StripSlashes`,
      `CleanPath`, `http.StripPrefix`); `FromHTTP`'ye giden gövde kablodaki **ham** baytlardır (önünde açma/decompress
      yok); imzalanmayan başlıklar (`Content-Type`, `Content-Encoding`) isteğin anlamını değiştirmez.
    - **Handler tarafı kemer:** Go tarafı redline R7 tetikleyicisi yeni handler dosyaları için `nonce`/`signature`'a
      genişletilir (önce gürültü ölçülür); bugün bu kelimeleri yalnız mobil kol (R7M) taşır.

> **Kart düzeltmesi (2026-11-14, M16-07 uygulaması sırasında).** Uç noktalar kesinleşti; sapmalar ve ölçümler:
> - **Uç noktalar:** `POST /app/v1/activate/lookup` (imzasız; `GET /activate`'in eşi — onay ekranının işvereni, kişiyi,
>   mekânı ve markayı göstermesi için; **kartta yoktu, eklendi**: GDPR Art. 13'ün veri sorumlusu onaydan önce
>   gösterilir, M16-12'nin *"onay ekranı (tarayıcı sihirbazının metni, tenant markası)"* maddesi onu ister) · `…/activate/bind`
>   (imzasız) · `…/activate/complete` (bağlı anahtarla imzalı) · `POST /app/v1/nonce`, `POST /app/v1/tap/start`,
>   `GET /app/v1/me` (**imzalı okuma**: nonce yerine `App-Timestamp`, kanonik önek `tappa-app-read-v1`, pencere sunucu
>   saatine göre **±60 sn** — `appdevice.ReadWindow`; kenarlar tabloda ölçüldü) · `POST /app/v1/tap`, `POST /app/v1/revoke`
>   (nonce'lu imza). Bütün imza retleri **401, boş gövde**; hata gövdesi kapalı sözlük (`internal/handler/app.go`
>   `appErrors`, 14 kod); her yanıt `no-store`.
> - **Bağ için migration GEREKMEDİ.** `bind` davetin var olan `consent_binding_hash`'ine yazar (ADR 0026 deseni). Bağ
>   değeri sunucunun çektiği 32 baytlık meydan okuma + aktivasyon kimliği + açık anahtardan türetilir
>   (`appdevice.ActivationBinding`, 43 karakter — `invite.Binding`'in şekli). `complete` mevcut `SignedRequest`
>   biçimindedir: `App-Device-Id` = aktivasyon kimliği, `App-Nonce` = meydan okuma; imza gövdenin adlandırdığı anahtarla
>   doğrulanır (`appdevice.VerifyActivation`, saf) ve aynı anahtarın bağını verir — **sahiplik kanıtı, sunucunun verdiği
>   meydan okuma üzerinde, `CreateAppDevice`'tan önce**. 00107 kullanılmadı (o numara sonra `main`'in OAuth kayıt migration'ına gitti; M16-19'un önerisi 00111).
> - **Sıra `CompleteByTap`'inkidir**, önüne yalnız saf imza denetimi girer: sel tavanı · sahiplik kanıtı · davet + davet
>   bütçesi · yalnız NFC · ilerletmeyen ön kontrol (aynı tenant, aktif, duvarda) · `sun.Verify` · tek transaction'da
>   tüketim + oturumlar + eski cihazlar + yeni cihaz (`invite.Manager.ActivateDevice`). `transactions` satırı yok.
>   Ölçüm: 8 eşzamanlı aynı istek → tam 1 başarı, `last_ctr` +1, tek canlı cihaz
>   (`TestAppDB_ConcurrentCompletesOfOneReadActivateExactlyOnce`). 23505 → `new_key_required`, davet harcanmaz
>   (`TestAppDB_ARegisteredKeyAsksForANewKeyAndSpendsNothing`).
> - **Q40 (a)** iki yarısıyla uygulandı, ikisi de tüketimin transaction'ında (ADR 0045 §2.3 notu).
> - **Nonce üst sınırı** `appdevice.MaxLiveNonces` = 8: aşılınca en eski düşer, istek reddedilmez. Cihaz satırı önceki bir
>   ifadede kilitlenir (`LockAppDeviceForNonce`, `FOR NO KEY UPDATE`), sonra `DropOldestLiveAppRequestNonces`, sonra veriş,
>   sonra ölü süpürme. **Mutasyonla ölçüldü:** kilit ifadesi atlanınca 32 eşzamanlı verişte 20–21 canlı nonce kaldı (test
>   kırmızı); kilitle ≤ 8. Nonce isteği imzalı olduğu için **yem nonce seçilmedi**; kalan durum kâhini M16-05'in sayılı
>   sınırıdır (bilinmeyen/iptal edilmiş cihaz imzadan önce reddedilir — ~0,2 ms'lik imza denetimi farkı + DB gürültüsü).
> - **Oran sınırı:** yüzeyin kendi `httpx.TapLimiter` örneği (JSON 429) — adres kovası her rotanın önünde (kaynak başına
>   sınır, nonce dahil), cihaz kovası (`AllowDevice`, oturum bütçesi) **yalnız imza doğrulandıktan sonra** (iddia edilen bir
>   kimliği yakmak gerçek telefonu kilitlerdi). Aktivasyonun üç rotası sihirbazın sel tavanını ve davet bütçesini
>   **paylaşır** (bir davet, bir bütçe).
> - **Yönlendirme:** `/app/v1` grubunda ve router'da yol yeniden yazan ara katman yok; gövde ham baytlardır;
>   `Content-Type`/`Content-Encoding` okunmaz (`TestApp_TheSignedPathIsTheWirePath`, `httpx.NewRouter` üstünden).
> - **R7'nin uygulama yüzeyi kolu** (`internal/handler/app*.go`: `signature`, `sig`, `nonce`, `challenge`, `public_key`,
>   `binding`): gürültü önce ölçüldü — 2 isabet, ikisi de mesaj düzyazısı, mesajlar yeniden yazıldı
>   (`TestRedline_M16_AppSurfaceLogCalls`).
> - **Q41 ölçümü:** `GET /app/v1/me`'nin plaketsiz tahmini `tenant.Directory.AppHome` → `predictTap`'tir — `GET /t`'nin
>   aynı fonksiyonu ve aynı kişi-ve-anahtar kapısı; plaket yarısı (`predictionAllowed`) uygulanamaz, gerekçesi
>   `apphome.go`'da. `TestAppHomeDB_ThePredictionWithoutAPlaqueIsTheTapPagesOwn` her durumda (açık giriş yok/var/eski,
>   anahtar kapalı/henüz değil, deaktive, işletmenin başka mekânı) `TapPage(predict=true)` ile **birebir aynı** sonucu ölçtü.
> - **İptal edilmiş cihazın dokunuş satırı M16-07'nindir** (ana oturum kararı, 2026-11-14; M16-09'un *"yalnız tenant,
>   çalışan, kanal"* maddesinin yerine): `tap/start` ve `tap` reddinde tek WARN satırı *"a signed-out app device tapped"*,
>   öznitelikleri yalnız **kanal** (`tap/start`'ta SUN sorgusunun, `tap`'te `unverified`) ve sunulan cihaz kimliğinin
>   SHA-256'sının **ilk 16 hex karakteri** — **tenant ve çalışan YOK**: iptal edilmiş cihaz imzadan önce reddedilir, kimlik
>   o anda imzasız bir başlıktır ve ondan atfetmek herkese başka işletmenin çalışanını adlandıran log satırı yazdırırdı.
>   Yanıt bilinmeyen cihazınkiyle aynı 401'dir (`TestAppDB_ASignedOutDevicesTapLeavesOneUnattributedWarning`).
>   Kimlik bilgisi türünü değiştiren aktivasyonun kilit sırası M16-09'unkidir: oturumlar → cihazlar → nonce'lar
>   (`invite.Manager.ActivateDevice`).
> - **Devredilenler:** (2) Logo baytlarının uygulamaya yolu bu kartta yok — `/me` ve `tap/start` logonun özetini ve kutusunu verir
>   (M16-13/M16-17). (3) M15-17'nin talep kartı uygulamanın sonuç yanıtında yok (M16-13). (4) Tarayıcının *"Activation
>   complete"* ekranındaki *"other phone signed out"* yalnız oturumları sayar; uygulama cihazı iptali ekran metnini
>   değiştirmedi (§9 sorusu).
> - **İnceleme turu (2026-11-14):** `complete`'e `sun.Verify`'dan önce ilerletmeyen bir **bağ ön kontrolü** eklendi
>   (`invite.Manager.ConsentBound`, `InviteConsentMatches` okuması) — imzalayabilen ama bağlanmamış bir anahtar artık
>   plaketin sayacını harcamaz; kapı yine tüketen ifadedir. `/app/v1` altında bilinmeyen yol `not_found` 404, yanlış
>   yöntem `method_not_allowed` 405 (JSON, `no-store`); kod başına tek durum (`appErrorStatus`). Cihaz kovası ve cihaza
>   bağlı tap bağlamının ret yönleri testlendi ve mutasyonla ölçüldü (`AllowDevice` kapatılınca ve `app-device:` öneki
>   düşürülünce testler kırmızı).
> - **CLAUDE.md §5 *"Dört kanıt"* cümlesi** bu kartın işiydi; yapıcı CLAUDE.md'ye dokunmadı, metin ana oturuma önerildi.

> **Tel sözleşmesi (M16-07, 2026-11-14) — M16-12 ve M16-16 bunun üstüne kurar.** Testlerle sabit:
> `internal/handler/app_test.go` (`TestAppContract_ResponseShapes`, `TestAppContract_RequestShapes`,
> `TestAppContract_ErrorStatuses`), başlık eşlemesi ve akış `internal/handler/app_db_test.go`'da.
>
> | Uç nokta | Kimlik | İstek gövdesi | Yanıt (200) |
> |---|---|---|---|
> | `POST /app/v1/activate/lookup` | imzasız | `{"code"}` | `{"employeeName","businessName","locationName"?,"wifiSsid"?,"secondDevice","brand"}` |
> | `POST /app/v1/activate/bind` | imzasız | `{"code","consent":true,"publicKey"}` | `{"activationId","challenge"}` |
> | `POST /app/v1/activate/complete` | `App-Device-Id` = `activationId`, `App-Nonce` = `challenge`, `App-Signature` (bağlı anahtar) | `{"code","publicKey","platform":"ios"\|"android","sun"}` | `{"deviceId","employeeName"}` |
> | `POST /app/v1/nonce` | imzalı okuma | boş ya da `{}` | `{"nonce","expiresAt"}` (RFC 3339, UTC) |
> | `POST /app/v1/tap/start` | imzalı okuma | `{"sun"}` | `{"employeeName","locationName","brand","direction","qr","context"}` |
> | `POST /app/v1/tap` | nonce'lu imza | `{"context","want"?: "in"\|"out","lat"?,"lng"?,"mockLocation"?,"occurredAt"?}` | `{"outcome":"recorded","verdict":"ok"\|"flag"\|"reject"\|"ignored","direction":"in"\|"out"\|"","venue","at":"HH:MM","businessType","practice"?}` |
> | `GET /app/v1/me` | imzalı okuma | boş | `{"employeeName","brand","direction":"in"\|"out"\|""}` |
> | `POST /app/v1/revoke` | nonce'lu imza | boş ya da `{}` | 204, gövde yok |
>
> - `brand` = `{"name"?,"logo"?:{"sha256","width","height"},"accent"?}` (accent `#`'sız 6 hex, ör. `1F5C41`); `{}` =
>   Taptime'ın kendi görünümü. `publicKey` = 65 baytlık ham P-256 noktasının dolgusuz base64url'i. `sun` = plaket
>   URL'sinin **sorgusu** (`tag=…&ctr=…&cmac=…`). `direction` `""` = yönsüz *"Tap"* düğmesi.
> - **İmza başlıkları:** okuma `App-Device-Id` + `App-Timestamp` (ondalık Unix saniyesi) + `App-Signature`; nonce'lu
>   istek `App-Device-Id` + `App-Nonce` + `App-Signature`; ikisi birlikte → 401. Kanonik metinler
>   `test/fixtures/appdevice_vectors.json`.
> - **Hatalar:** imza reddi her zaman **401, boş gövde**; diğerleri `{"error":<kod>}`, kod başına tek durum:
>   `bad_request` 400 · `invalid_invite` 400 · `consent_required` 400 · `invalid_key` 400 · `activation_needs_touch` 400 ·
>   `activation_tap_failed` 400 · `stale_context` 400 · `foreign_tenant` 403 · `unknown_plaque` 404 · `not_found` 404 ·
>   `method_not_allowed` 405 · `new_key_required` 409 · `activation_not_ready` 409 · `try_again` 409 · `rate_limited` 429
>   (`Retry-After`) · `server_error` 500. Her yanıt `Cache-Control: no-store` ve `Date` taşır.
> - **Sürümleme (ana oturum kararı, 2026-11-14):** çözümleme **katıdır** — bilinmeyen alan 400 (`DisallowUnknownFields`).
>   Kural: **yeni bir alan gönderen herhangi bir uygulama sürümünden ÖNCE sunucu sevk edilir.** ⚠️ `encoding/json` alan
>   adlarını **büyük/küçük harfe duyarsız** eşler (`"Code"` = `"code"`, ölçüldü); uygulamalar tablodaki yazımı kullanır.

## M16-08 — `/.well-known` dosyaları (Universal Link / App Link)

- **Bağımlılık:** M16-01 · kullanıcı: Apple Team ID (**kullanıcıdan alınacak** — hesap Everva Bilişim'in), paket
  adı (Q35), imza sertifikası parmak izi
- **Kırmızı çizgi:** — (plaket URL'sini hangi uygulamanın yakalayabildiğini belirler)
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `feat(app): publish the app association files`

**Amaç.** `https://taptime.mt/.well-known/apple-app-site-association` ve `/.well-known/assetlinks.json`'u Go
ikilisinden sunmak; yalnız `/t` (ve Faz D'nin canlı QR yolu — M16-20 ekler) uygulamaya bağlanır.

**Kabul kriterleri.**
- Değerler config'ten (env), yoksa yol 404 — eksik env **başlangıçta panic değil** (uygulama isteğe bağlı;
  `internal/config`'in isteğe bağlı küme deseni, ör. operatör yüzeyi).
- `apple-app-site-association` `Content-Type: application/json`, yönlendirmesiz (Apple yönlendirmeyi izlemez —
  **doğrula**); `www` ve eski host'lar yönlendirmede (`43-redirect-ingress.yaml`) — Universal Link'in yalnız
  `taptime.mt` için çalıştığı yazılır.
- Ingress bu yolları uygulamaya iletir; ACME çözücüsünün `/.well-known/acme-challenge/<token>` yolu yalnız sertifika
  çözülürken eklenir (`deploy/k8s/42-api-ingress.yaml:22`, `43-redirect-ingress.yaml:48` yorumları; `40-ingress.yaml`
  bunu anmaz) — `taptime.mt` host'unda `/.well-known/*`'in uygulamaya ulaştığı canlıda ölçülür.
- Faz D'de canlı QR yolu eklenince (M16-20) iki dosyanın yol listesi **o kartta** güncellenir.
- `/activate` uygulamaya **bağlanmaz** (öneri): davet linki tarayıcıda açılır, uygulama aktivasyonu kendi ekranından
  davet kodunu alır — tarayıcı aktivasyonu uygulama kurulu diye değişmesin (Q40 ile birlikte karar).

> **Kart düzeltmesi (2026-11-14, M16-08 uygulaması sırasında).** Uygulandığı biçim ve kartın metinden saptığı yerler:
> (1) **İki env, birbirinden bağımsız** (küme değil): `TAPPA_APP_APPLE_TEAM_ID` (10 büyük harf/rakam; app ID
> `<TeamID>.mt.taptime.app`) ve `TAPPA_APP_ANDROID_SIGNER_SHA256` (adında `CERT` yok: `TestPackaging_ConfigNamesNoCertificateVariable` bu kökü
> bir güven deposu sayar, ADR 0022 §2; virgülle liste; her biri Play Console biçimi, 32 büyük
> harf hex çifti iki noktayla). Platformlar ayrı zamanlarda çıkar (önce Android), bu yüzden her biri yalnız kendi
> dosyasını açar. **Boş = o dosya 404, sunucu normal açılır;** *set edilmiş ama bozuk* değer açılışı reddeder
> (`TAPPA_TAG_KEK_PREVIOUS` kuralı — bozuk isteğe bağlı değer "yok" sayılmaz). `05-config.yaml`'da ikisi de **boş**
> (`TestPackaging_TheAppValuesAreTheHandedOverOnes`); değer uydurulmaz (Q35). Paket/bundle kimliği `mt.taptime.app` koddadır
> (`handler.AppBundleID`) — mağaza kimliği yapılandırma değil karardır. (2) **Yönlendirmesizlik doğrulandı (belgeden):**
> Apple, *"Supporting associated domains"*: *"You must host the file using https:// with a valid certificate and with no
> redirects."* Android, *"Configure assetlinks.json"*: *"must be accessible without any redirects (no 301 or 302
> redirects)"* ve *"served with content-type `application/json`"*. Apple'ın sayfası Content-Type söylemiyor; iki dosya da
> `application/json` ile sunulur. Apple dosyayı cihazdan değil **kendi CDN'inden** çeker (iOS 14+; *"within 24 hours"*,
> cihazlar *"approximately once per week"*) — canlı ölçümü CDN üstünden yapılır (**doğrula**). (3) **Yalnız
> `taptime.mt`:** `www.taptime.mt` ve `tappa.everva.com.tr` uygulamaya ulaşır ama her isteğe 301 alır (T71,
> `internal/httpx/redirecthost.go`); yönlendirme ilişkilendirme değildir, dolayısıyla **Universal Link ve App Link yalnız
> `taptime.mt` için çalışır** — plaketin URL'si de yalnız onu taşır. Operatör ve API host'larında iki yol 404'tür
> (kendi host kapıları), bilinmeyen bir host'ta da (handler'ın kendi host denetimi, `httpx.OnHost`). (4) **Android'in
> "yol listesi" dosyada değil manifesttedir:** klasik `assetlinks.json`'da yol yoktur; yolu uygulamanın intent-filter'ı
> belirler (M16-11 `android:path="/t"` yazmalı). Ek olarak dosya aynı listeyi Android 15+'ın *Dynamic App Links*
> uzantısıyla taşır (`relation_extensions` → `dynamic_app_link_components: [{"/": "/t"}]`; manifestin kapsamını
> daraltabilir, genişletemez; Android ≤14 yok sayar). Faz D'de (M16-20) güncellenecek liste tektir:
> `internal/handler/applinks.go`'daki `appLinkPaths` — **artı** Android manifesti. (5) **Ingress değişmedi:**
> `40-ingress.yaml`'ın `/` Prefix'i `/.well-known/*`'i zaten uygulamaya iletir; ağaçta `taptime.mt` üstünde
> `/.well-known` altında statik yol **yoktur** ve olmamalıdır — ACME çözücüsünün geçici
> `/.well-known/acme-challenge/<token>` yolu daha özel olduğu için yalnız çözüm sırasında kazanır
> (`TestPackaging_TheAppFilesAreRoutedToTheAppOnTheCanonicalHost`, YAML'ın statik okuması; canlıda ölçülmedi).
> (6) `.well-known` `plainNotFoundRoots`'a girdi: yapılandırılmamış dosya ve bilinmeyen `/.well-known/…` yolu genel 404
> sayfasını değil düz 404'ü alır. (7) **Statik plaket QR'ı da `/t`'dir** (`sun.PlaqueQRURL`: `/t?tag=<UID>`): bileşen
> sorguya bakmadığından uygulama kuruluyken kamera ile okunan QR da uygulamayı açar — M16-22'nin (Q36 c) istediği bu,
> ama **M16-12/13 (Android) ve M16-16/17 (iOS) ilk sürümden SUN'suz `/t?tag=` URL'sini karşılamalıdır** (QR kanalı
> ya da tarayıcıya devretme). **Sıra kuralı:** bir platformun env değeri ancak o platformun uygulaması `/t`'yi —
> sorgusuz QR biçimi dahil — karşıladıktan **sonra** doldurulur; önce doldurulursa her dokunuş onu karşılayamayan
> bir uygulamayı açar.
> (8) **Kart "Kırmızı çizgi: —" diyordu; R1'e değdi.** Google'ın anahtarı `"sha256_cert_fingerprints"` R1'in
> `fingerprint` kökünü tetikler (ölçüldü: 38 satır FAIL). Ad her yerde *certificate digest / CertSHA256* oldu; kalan
> iki satır (struct etiketi, altın satır) için `scripts/redline-check.sh`'e **tırnaklı anahtara ve yalnız
> `internal/handler/applinks.go` + `applinks_test.go`'ya bağlı**, pazarlama ifadeleriyle çarpılmayan kendi ifade→yol
> çifti eklendi (`R1_PROTOCOL_PHRASES`/`R1_PROTOCOL_PATHS`; denetim sonrası daraltıldı) — ADR 0012'nin M16-08 eki, sonda
> testi `TestRedline_M16_08_TheAssetLinksKeyIsTheOnlyWaiver`. M16-27'nin runbook'u (`keytool` çıktısı) aynı köke
> çarpar: `docs/` taranmaz, ama `deploy/` ve `mobile/` taranır.

## M16-09 — "Telefonlarını çıkar" uygulama cihazlarını da kapatır; deaktivasyon kapatmaz

- **Bağımlılık:** M16-03, M16-05
- **Kırmızı çizgi:** §4.5, §4.6, §5 satır 4
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(employees): sign out an employee's app devices too`

**Amaç.** Tarayıcı oturumunun bugünkü iki kuralını uygulama cihazına aynen taşımak: müdürün *"telefonlarını çıkar"*ı
cihazı da kapatır; **deaktivasyon hiçbir şeyi iptal etmez**.

**Neden deaktivasyon iptal etmez (kullanıcı kararı 2026-08-07).** `db/queries/sessions.sql:85` *"DEACTIVATION MUST NOT
CALL THIS"*; `internal/domain/tenant/staff.go:40-43`; `internal/handler/employeeactions.go:563-567`. Deaktive kişinin
oturumu bilerek canlı kalır ki dokunuşu §5 satır 4'e ulaşsın ve **kaydedilsin** (`internal/handler/tap.go:446-454`);
iptal o dokunuşu satır 3'ün kayıtsız yoluna iterdi. T59 deaktive bir kişinin telefon çıkışını da reddeder
(`tenant.ErrSignOutDeactivated`, `employeeactions.go:661`). Handoff §2'nin *"offboarding: tek tık → oturum o saniye
ölür"* cümlesi bu karardan önce yazılmıştır; deaktivasyonun etkisi oturumun ölmesi değil, sonraki her dokunuşun
**reddedilip kaydedilmesidir**.

**Kabul kriterleri.**
- Panelin telefon çıkışı (`employeeactions.go:690`, T59) aynı transaction'da uygulama cihazlarını da iptal eder; log
  satırı iki sayıyı da verir; deaktive kişide `ErrSignOutDeactivated` aynen (uygulama cihazı da dokunulmaz).
- **Deaktivasyon (ADR 0010) uygulama cihazını iptal ETMEZ** — `tenant.Staff.Deactivate`'e çağrı eklenmez; bir test
  deaktivasyondan sonra cihazın `revoked_at IS NULL` olduğunu ölçer.
- Deaktive çalışanın uygulamadan dokunuşu §5 satır 4: `reject` + **kayıt** + güvenlik uyarısı — `employees.status` +
  `sys:employee-deactivated`'dan (DB testi, tarayıcı ikiziyle aynı sonuç).
- **İptal edilmiş cihazın dokunuşu §5 satır 3'ün ikizi:** 401 / yeniden aktivasyon, **kayıt yok** (DB testi:
  `transactions` sayısı değişmez), ve tarayıcının `"tap page: a signed-out phone tapped"` uyarısının (`tap.go:446-454`)
  eşi bir WARN satırı — **M16-07'nin `/app/v1` handler'ında**: mesaj `"a signed-out app device tapped"`, alanlar **yalnız**
  kanal ve sunulan cihaz kimliğinin SHA-256'sının ilk 16 hex karakteri; **tenant ve çalışan yok**, çünkü o noktada cihaz
  kimliği başlığı imzası doğrulanmamış bir iddiadır (ana oturum kararı, 2026-11-14 — aşağıdaki kart düzeltmesi (2)).
- İptal tek yönlüdür (`00099` deseni; tetikleyici ya da yetki).
- Panel metni (§9 değil, panel): *"Sign out phones"* artık uygulamayı da kapsadığını söyler — metin değişikliği
  kullanıcıya gösterilir.

> **Kart düzeltmesi (2026-11-14, M16-09 uygulaması sırasında).** (1) **Atomiklik ölçüsü** (Q40 (a) — tek "kim"
> kimliği): oturumlar, cihazlar (+ canlı nonce'ları, aynı ifadede) ve `audit_log` satırı **tek transaction**'dadır —
> kısmi sonuç yoktur; hata = 500 + hiçbir şey değişmez. 40P01'de transaction **bütünüyle** en çok 3 kez yeniden
> denenir (`signOutAttempts`; M16-03'ün *"iptal çağıranı 40P01'de yeniden dener"* kuralı) — deterministik test
> `TestStaffDB_ADeadlockedSignOutIsRetriedNotShown`. Kilit sırası: `sessions` → `app_devices` → `app_request_nonces`;
> kimlik türünü değiştiren aktivasyon (M16-07) aynı sırayı almalı. (2) **İptal edilmiş cihazın WARN satırı — ana oturum
> kararı (2026-11-14, M16-09 incelemesi, ORTA bulgu):** satır **M16-07'nin `/app/v1` handler'ında** yazılır (401 yanıtıyla
> birlikte): mesaj `"a signed-out app device tapped"`, seviye WARN, alanlar **yalnız** kanal ve sunulan cihaz kimliğinin
> SHA-256'sının ilk 16 hex karakteri. **Tenant ve çalışan YAZILMAZ** — kartın ilk metnindeki *"yalnız tenant, çalışan,
> kanal"* bu karar tarafından **değiştirildi** (kabul kriteri yukarıda yeniden yazıldı): o noktada cihaz kimliği başlığı imzası doğrulanmamış bir **iddiadır**
> (`ErrRevokedDevice` imzadan önce karar verilir, `internal/appdevice/appdevice.go`), ondan tenant/çalışan atamak herkese
> başka bir işletmenin çalışanını adlandıran log satırı yazdırırdı. Tarayıcının satırı tenant/çalışan taşıyabilir, çünkü
> çerez kimlik bilgisinin kendisidir. Bu kart doğrulayıcı + `checkin.Service.Record` düzeyini ölçer
> (`TestAppDeviceDB_ASignedOutDeviceIsRefusedAndRecordsNothing`, `TestAppDeviceDB_ADeactivatedPersonsAppTapIsRejectedAndRecorded`).
> (3) **Kartın düğmeyi sunması için sayım gerekti:** `GetPanelPersonSummary`'ye `live_app_devices` eklendi; yalnız
> uygulamada oturumu olan kişiye de *"Sign out their phones"* sunulur. Kadro satırının sayımı tarayıcıda kalır →
> M16-26 (cihaz listesi). (4) **Uygulama ifadesi yalnız canlı uygulama cihazının yanında** (incelemenin DÜŞÜK bulgusu;
> Q42 açık): `LiveAppDevices = 0` olan her ekran — kart, uyarı, *"signed-out"*, *"no-phones"*, *"signout-deactivated"* —
> T59'un metnini kelimesi kelimesine okur; uygulama cihazı olan kişide tarayıcı satırı *"In the browser: none signed in"* (küçük harf).

## M16-10 — Faz A kapanışı

- **Bağımlılık:** M16-02 … M16-09
- **Araç:** yapıcı + üçüncü göz · **Mercek: iki**
- **Commit:** `test(app): sign the app surface end to end`

**Amaç.** Faz A'nın sunucu yüzeyini, uygulama yokken, imzalı bir test istemcisiyle uçtan uca kanıtlamak ve fazın büyük
testlerini bir kez koşmak.

**Kabul kriterleri.**
- Go'da imzalı bir **sahte istemci** (test yardımcısı; M16-05'in vektörleriyle) gerçek Postgres'e karşı: aktivasyon
  (bind → complete) → nonce → tap/start → tap → kayıt (`client='app'`) → tekrar (60 sn, `ignored`) → telefon çıkışı →
  401, **kayıt yok** (satır 3'ün ikizi) · ayrı bir kişide deaktivasyon → dokunuş `reject` + **kayıt** (satır 4). Aynı testte tarayıcı yolu bir satır yazar ve `client='browser'` okunur; manuel kayıt `client IS NULL`
  (M16-04'ün CHECK'siz kanıtı).
- Bu fazın **büyük testleri bir kez:** tam `-race`, `go test ./...`, `make check`, `make audit`.
- ADR 0045'e *"Faz A uygulandı"* notu (migration numaraları, ölçülen sapmalar).
- `state.md`: Faz A done; sevk kararı kullanıcıya sorulur.

---

# Faz B — Android MVP (`mobile/android/`)

## M16-11 — Android iskeleti

- **Bağımlılık:** M16-10 · Q35 (paket adı), Q36 (Compose artefaktları)
- **Kırmızı çizgi:** §1 (bağımlılık), §4.1, §4.2
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `feat(android): scaffold the staff app`

**Amaç.** `mobile/android/`'de, yalnız onaylanmış bağımlılıklarla ve yalnız izinli izinlerle derlenen, kırmızı çizgi
taramasından geçen boş bir personel uygulaması.

**Kabul kriterleri.**
- Ayrı Gradle build `mobile/android/`; toolchain `android/README.md`'nin **ölçülmüş** sürümleri (JDK 21, Gradle 9.7.1,
  AGP 9.4.1, compileSdk 36, minSdk 26) — sapma varsa ölçülerek yazılır; wrapper JAR yok (*"bu repo ikili taşımaz"*).
- Bağımlılıklar yalnız Q36'da onaylananlar; `debugRuntimeClasspath` dökümü `mobile/android/README.md`'de.
- **M16-02 son güvenlik kontrolünün devri (DÜŞÜK, 2026-11-14):** `rg` git index'ine bakmadan `.gitignore`'a uyar; `mobile/` altında ignore'a takılıp `git add -f` ile izlenen bir dosya (ör. `mobile/.idea/Leak.kt`) hiçbir mobil kural tarafından taranmaz, S1 de `mobile/`'ı muaf tuttuğu için görmez. Bu kart `mobile/android/` kendi ignore kalıplarını getirdiğinde bekçiyi ekler: `git ls-files -ci --exclude-standard -- mobile android` boş değilse FAIL (ya da adıyla sınır olarak yazar). Aynı özellik SRC'nin geneli için tabanda da vardı.
- Manifest yalnız: `NFC`, `INTERNET`, `ACCESS_FINE_LOCATION` (+ `ACCESS_COARSE_LOCATION`). **Birleştirilmiş**
  manifestte (`processDebugManifest` çıktısı) başka izin yok — bir JVM testi ya da betik bunu ölçer.
- `allowBackup=false` (anahtar zaten yedeklenmez; uygulama verisi de taşınmasın).
- `make mobile-android` / `make mobile-android-test` hedefleri (mevcut `make android` desenine paralel).
- `bash scripts/redline-check.sh` yeşil (M16-02'nin kalıpları artık gerçek dosyaları tarar).

## M16-12 — Android cihaz anahtarı ve aktivasyon

- **Bağımlılık:** M16-11
- **Kırmızı çizgi:** §4.1, §4.7, §5 (aktivasyon)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(android): activate with a hardware-bound key`

**Amaç.** Android'de dışa aktarılamayan bir Keystore anahtarı üretip ADR 0026'nın eşi olan iki adımlı aktivasyonu
(bind → ilk gerçek NFC okumasıyla complete) uçtan uca çalıştırmak.

**Kabul kriterleri.**
- Keystore'da P-256 imza anahtarı; StrongBox varsa orada (yoksa TEE; sonuç kaydedilir); `setUserAuthenticationRequired`
  **çağrılmaz**; dışa aktarma yok.
- Aktivasyon akışı ADR 0026'nın eşi: davet kodu → onay ekranı (tarayıcı sihirbazının metni, tenant markası) →
  `/app/v1/activate/bind` → *"plakete dokun"* → ilk gerçek NFC okuması → `/app/v1/activate/complete` (M16-07'nin
  `CompleteByTap` sırası; sunucu `sun.Verify` ile doğrular, uygulama doğrulamaz).
- M16-05'in bilinen cevap vektörleri Kotlin tarafında JVM testinde geçer (aynı kanonik metin).
- CLAUDE.md §5 *"Aktivasyon dokunuşu"*na uygulama yolu cümlesi (M16-16 ile ortak; ilk biten yazar, ikinci uyarlar).
  **§5 sıra notu:** CLAUDE.md §5 *"Aktivasyon dokunuşu"* bugün sırayı *"`sun.Verify` → aktif plaket → tüketim"*
  diye yazar (`CLAUDE.md:391`); ADR 0026 md. 3 ve plan ise *"ilerletmeyen ön kontrol (aynı tenant, aktif) → `sun.Verify`
  → tüketim"* der, ve `internal/handler/activate.go:852-862`'nin ORDER yorumu eski sırayı (`sun.Verify` → plaket
  denetimleri) taşır. Cümleyi yazan kart sırayı kodun **ölçülen** sırasına eşitler; yorum ile metin çelişiyorsa
  ikisi aynı commit'te düzeltilir.

> **Saat kayması sözleşmesi (M16-07 incelemesi, 2026-11-14).** İmzalı okumanın penceresi sunucu saatine göre ±60 sn'dir;
> saati bundan fazla kaymış bir telefon nonce bile alamaz ve 401'i iptal edilmiş cihazınkiyle **aynıdır**. Uygulama bu
> yüzden: (1) her `/app/v1` yanıtının `Date` başlığından (her yanıtta var — 200, 401, 404/405 dahil; test:
> `TestApp_UnknownPathsAndMethodsAnswerTheVocabulary`, `TestApp_TheSignedPathIsTheWirePath`) sunucu saatine göre
> farkı hesaplar ve **düzeltilmiş saatle** imzalar; (2) bir 401'i *"oturum kapandı / yeniden aktive ol"* diye ancak taze
> bir fark ölçümüyle **bir kez yeniden denedikten** sonra yorumlar.

> **M16-11 devri (denetim B-1, 2026-11-18).** Q36 (d) *"Android JVM testleri CI'da"* bu kartındır: `ci.yml`'e
> `mobile/android` için `testDebugUnitTest` işi (yalnız birinci parti action'lar — `ci.yml:7-8`; JDK `actions/setup-java`,
> Gradle 9.7.1 sağlama toplamıyla indirilir, runner imajındaki SDK 36 ölçülür). Rölenin (`android/`) testleri de aynı işe
> girebilir. Kabul: dal push'unda iş yeşil ve `MergedManifestTest` + bu kartın vektör testleri CI çıktısında görünür.

## M16-13 — Android tap ekranı: NFC, tek seferlik konum, sahte konum

- **Bağımlılık:** M16-12, M16-08
- **Kırmızı çizgi:** §4.2, §5, §9
- **Araç:** yapıcı + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(android): clock in from a plaque tap`

**Amaç.** Plakete dokunan Android telefonun uygulamada tarayıcı tap ekranının eşini açması ve dokunuşu imzalı olarak
`/app/v1/tap`'a kaydettirmesi.

**Kabul kriterleri.**
- `NDEF_DISCOVERED` filtresi `https://taptime.mt/t` için; uygulama tarayıcıdan önce seçilir (**doğrula** gerçek
  cihazda, M16-14); App Link (`autoVerify`) QR/bağlantı açılışları için.
- Ekran tarayıcı tap ekranının eşi: ad, tahmini yön düğmesi, *"Not right?"*, tenant logosu + accent slotu; sonuç
  ekranı düğmesiz (§9). Farklılık varsa kullanıcıya sorulur.
- Konum yalnız düğmeye basıldığında, tek seferlik; izin reddi akışı tarayıcıdaki *"konumsuz"* yolun aynısı (kayıt
  yine yazılır, §4.6). **Doğrula:** platform `LocationManager.getCurrentLocation` API 30+'dır; minSdk 26 ile 26–29
  arasında `requestSingleUpdate` (API 30'da kullanımdan kalktı) gerekir. Fused Location Provider Play Services ister ve
  Q36'da **yoktur** — kullanılacaksa önce sorulur.
- Sahte konum bayrağı (`isMock()` / `isFromMockProvider()`) yalnız true ise gönderilir.
- Ağ hatası: dokunuş kaybolmaz mı? — MVP'de **kuyruk yok**; ekran *"tekrar dokun"* der ve plaket sayacı bir sonraki
  dokunuşta ilerler (A1'in sinyali `ctrGap` bunu sayar — bilinen bedel yazılır).
- Log satırlarında koordinat, imza, nonce yok (JVM testi).
- Konum çağrıları (`getCurrentLocation` / `requestSingleUpdate`) **yalnız tap akışı dosyasında**; bir çağrı-yeri izin
  listesi testi bunu pinler (M16-02 denetimi: tek seferlik API bir zamanlayıcıyla periyodik takibe dönüşebilir ve
  `redline-check.sh` R2'nin periyodik ağı yalnız aynı dosyayı görür).

**Tuzaklar.** UI zamanlayıcıları (ör. `while (isActive)` / `while (true)` ile bir geri sayım coroutine'i,
`scheduleAtFixedRate`) tap akışındaki konum çağrısıyla **aynı dosyada** olursa `redline-check.sh` R2'nin periyodik ağı
FAIL verir (M16-02: dosya bazlı ağ) — zamanlayıcılar ayrı bir görünüm dosyasında yaşar.

> **Logo notu (M16-07 incelemesi, 2026-11-14).** Uygulamaya logo baytlarının yolu bu kartla (ya da eşiyle) eklenince
> **eş işletme plaketi** ayrımı gerekir: tarayıcıda `tenant.PageBrand.PeerTag` doluysa logo `/t/logo/<sha>?tag=<uid>`
> (eşin logosu, `holding_logo` üzerinden) yolundan gelir, değilse oturumun kendi işletmesinin yolundan. `tap/start`'ın
> `brand`'i bugün bu ayrımı taşımaz; logo yolu eklenirken taşımalıdır.

> **M16-11 devri (denetim B-3, 2026-11-18).** Konum izinleri Play'de `android.hardware.location`'ı örtük **zorunlu**
> özellik yapar (`aapt2 dump badging`: `uses-implied-feature … required`) — konum donanımı olmayan cihaz uygulamayı
> göremez. Bu kart ya da M16-27 `<uses-feature android:name="android.hardware.location" android:required="false"/>`
> yazıp yazmamaya karar verir. `android.hardware.nfc` zaten zorunlu değil (NFC'siz telefon kurabilir, ADR 0045).

> **M16-12 devirleri (yeniden denetim, 2026-11-18).** (1) API 31+'da `SECURITY_LEVEL_UNKNOWN` bugün kabul edilir
> (`KeystoreKeys.kt` `backingOf` → `UNKNOWN`, `Ports.kt` `KeyGate`): ucuz sertleştirme, UNKNOWN'da `isInsideSecureHardware`'e
> de bakmak. (2) `InviteCode` port'a bakmaz, `PlaqueRead` bakar — tutarlı hale getir. (3) Keystore okunamazsa Android
> genel sunucu metnini gösterir; iOS'un `keyUnreadable` metni + *Try again* eşi. (4) `backingOf` hata verirse yeni
> anahtar yetim kalır. (5) **Tap ekranı dokunuştan önce kişinin adını gösterir** — ADR 0045 kabul edilmiş riskin
> (paylaşılan telefon) hafifletmesi, kullanıcı kararı 2026-11-18.

## M16-14 — Faz B kapanışı — gerçek cihaz turu

- **Bağımlılık:** M16-13
- **Araç:** yapıcı (kullanıcıyla cihaz başında) + üçüncü göz · **Mercek: iki**
- **Commit:** `docs(android): record the device tour`

**Amaç.** Android MVP'sini gerçek bir telefon ve gerçek bir plaketle senaryo senaryo ölçmek ve fazın büyük testlerini bir
kez koşmak.

**Kabul kriterleri.**
- NFC'li Android'de, staging ya da yerel sunucuya karşı gerçek bir test plaketiyle: aktivasyon · giriş · 60 sn tekrar
  (`ignored`) · çıkış · uçak modu · konum izni reddi · sahte konum uygulamasıyla `flag` · telefon çıkışı sonrası
  yeniden aktivasyon ekranı (**kayıt yok** — `transactions` sayısı değişmez) · deaktivasyon sonrası `reject` + kayıt.
  Deaktivasyon geri alınamaz (ADR 0010): bu adım **yalnız bu tur için açılan tek kullanımlık bir test çalışanıyla**
  koşar, gerçek bir çalışanla asla.
  Kayıt yazan her biri DB satırıyla (`client`, `channel`, verdict), kayıt yazmayanlar `transactions` sayısının
  değişmediğiyle kanıtlanır. **Test plaketinin `tags` satırı hiçbir temizlikte silinmez** (silinirse çip kalıcı kilitlenir).
- Tarayıcı yolu aynı telefonda uygulama kaldırılınca aynen çalışır.
- Fazın büyük testleri bir kez: mobil JVM paketi tamamı, `make check`, `make audit`.
- *Doğrula* maddelerinin sonuçları ADR 0045'e not.

> **M16-13 devirleri — cihaz turunda ölç (2026-11-18).** (1) NFC dağıtımında uygulamanın tarayıcıdan önce seçildiği ve
> App Link doğrulaması (assetlinks boşken davranış). (2) Recents / süreç ölümü sonrası plaket intent'inin yeniden
> işlenmediği (`FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY`). (3) API 30+'da `requestSingleUpdate`'in önbellek penceresinde
> gerçekten taze fix verdiği; iç mekânda API 26–30 GPS + network yarışı. (4) `org.json` adaptörü telefonda (JVM'de stub).
> (5) StrongBox → TEE düşüşü ve `securityLevel` okuması gerçek cihazda. (6) Basıştan sonra Back (API 26–30) çökmüyor.
> (7) DB'de `client='app'`, `channel='nfc'`, `sun_valid=true`. (8) `RedactionTest` ad listesi dar (DÜŞÜK, devir).

---

# Faz C — iOS MVP (`mobile/ios/`)

## M16-15 — iOS iskeleti

- **Bağımlılık:** M16-10 · Q35 (bundle kimliği), kullanıcı: Apple Team ID (**kullanıcıdan alınacak**)
- **Kırmızı çizgi:** §1, §4.1, §4.2
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `feat(ios): scaffold the staff app`

**Amaç.** `mobile/ios/`'ta, üçüncü parti paketsiz, yalnız izinli yetki ve `Info.plist` anahtarlarıyla derlenen, iPhone 7'yi
kapsayan boş bir personel uygulaması.

**Kabul kriterleri.**
- Xcode projesi `mobile/ios/`; Swift Package / CocoaPods bağımlılığı **yok** (Q36 aksini demedikçe) — CoreNFC,
  CryptoKit, CoreLocation, DeviceCheck, AVFoundation platformdandır.
- Dağıtım hedefi iPhone 7'yi kapsar (**doğrula**: iPhone 7'nin son iOS sürümü; SwiftUI ve `sourceInformation` o
  sürümde var mı).
- `Info.plist`: `NFCReaderUsageDescription`, `NSLocationWhenInUseUsageDescription` — **başka konum anahtarı yok**,
  `UIBackgroundModes` yok, `NSFaceIDUsageDescription` yok. Yetkiler: Associated Domains (`applinks:taptime.mt`), NFC
  Tag Reading (yalnız NDEF), App Attest (Faz E'ye kadar kapalı).
- Komut satırından derleme ve birim testi (`xcodebuild … test`) `make mobile-ios-test` hedefiyle; bu makinede ölçülür.
- `bash scripts/redline-check.sh` yeşil.
- **Derlenmiş** `Info.plist`'te arka plan modu ve *Always* izni yok: derleme çıktısında
  `plutil -extract UIBackgroundModes raw <app>/Info.plist` anahtarı bulamaz (çıkış ≠ 0) ve `NSLocationAlways*` anahtarı
  yok — `make mobile-ios-test` bunu ölçer (M16-02 denetimi: R2 kaynak metne bakar; xcconfig / `INFOPLIST_KEY_*` /
  proje üreteci modları başka yazımla birleştirebilir).

## M16-16 — iOS cihaz anahtarı ve aktivasyon

- **Bağımlılık:** M16-15
- **Kırmızı çizgi:** §4.1, §4.7, §5
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(ios): activate with a Secure Enclave key`

**Amaç.** iOS'ta biyometrisiz bir Secure Enclave anahtarı üretip Android'le aynı iki adımlı aktivasyonu çalıştırmak.

**Kabul kriterleri.**
- Secure Enclave P-256 anahtarı, `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`; `SecAccessControlCreateFlags`'te
  biyometri ya da `userPresence` **yok** (birim testi bayrakları okur).
- Aktivasyon akışı M16-12'nin eşi (aynı ekran sırası, aynı metinler); M16-05 vektörleri birim testinde geçer.
- Simülatörde Secure Enclave yoktur: test yolu bunu açıkça ayırır (sahte anahtar yalnız test hedefinde).
- CLAUDE.md §5 *"Aktivasyon dokunuşu"* cümlesi M16-12 ile ortaktır (ilk biten yazar).
  **§5 sıra notu:** CLAUDE.md §5 *"Aktivasyon dokunuşu"* bugün sırayı *"`sun.Verify` → aktif plaket → tüketim"*
  diye yazar (`CLAUDE.md:391`); ADR 0026 md. 3 ve plan ise *"ilerletmeyen ön kontrol (aynı tenant, aktif) → `sun.Verify`
  → tüketim"* der, ve `internal/handler/activate.go:852-862`'nin ORDER yorumu eski sırayı (`sun.Verify` → plaket
  denetimleri) taşır. Cümleyi yazan kart sırayı kodun **ölçülen** sırasına eşitler; yorum ile metin çelişiyorsa
  ikisi aynı commit'te düzeltilir.

> **Saat kayması sözleşmesi (M16-07 incelemesi, 2026-11-14).** İmzalı okumanın penceresi sunucu saatine göre ±60 sn'dir;
> saati bundan fazla kaymış bir telefon nonce bile alamaz ve 401'i iptal edilmiş cihazınkiyle **aynıdır**. Uygulama bu
> yüzden: (1) her `/app/v1` yanıtının `Date` başlığından (her yanıtta var — 200, 401, 404/405 dahil; test:
> `TestApp_UnknownPathsAndMethodsAnswerTheVocabulary`, `TestApp_TheSignedPathIsTheWirePath`) sunucu saatine göre
> farkı hesaplar ve **düzeltilmiş saatle** imzalar; (2) bir 401'i *"oturum kapandı / yeniden aktive ol"* diye ancak taze
> bir fark ölçümüyle **bir kez yeniden denedikten** sonra yorumlar.

## M16-17 — iOS tap ekranı: Universal Link + Core NFC ön plan okuma

- **Bağımlılık:** M16-16, M16-08 · Q41 (§9)
- **Kırmızı çizgi:** §4.2, §5, §9
- **Araç:** yapıcı + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(ios): clock in from a plaque, on every NFC iPhone`

**Amaç.** NFC'li her iPhone'da — arka planda okuyan XS+ ve yalnız ön planda okuyan 7–X — plaket dokunuşunu gerçek SUN
kanıtıyla kaydettirmek (P6'nın kapanışı).

**Kabul kriterleri.**
- iPhone XS+: arka plan okuması → Universal Link → uygulama açılır → tap ekranı (**doğrula** gerçek cihazda).
- iPhone 7–X: tap ekranı `GET /app/v1/me`'nin tahmini yönüyle çizilir; tek düğme Core NFC NDEF oturumunu açar, okunan
  URL `/app/v1/tap/start` + `/app/v1/tap`'a gider (Q41'in cevabına göre tek ya da iki dokunuş). `channel='nfc'`,
  `sun_valid=true` DB'de ölçülür — P6'nın kapandığının kanıtı.
- Konum: *When-In-Use*, düğmeye basınca tek seferlik (`requestLocation`; `startUpdatingLocation` ve
  `CLLocationUpdate` M16-02'nin R2'sine takılır); `sourceInformation
  .isSimulatedBySoftware` (iOS 15+) true ise bayrak (**doğrula**).
- Sonuç ekranı düğmesiz; *"tarayıcıda aç"* çıkışı yalnız hata/aktive değil ekranında (ADR 0045 Bedel 8; §9 sorusu).
- CLAUDE.md §9'a uygulama ekranlarının eşlik cümlesi (bu kartla ya da M16-13 ile — ilk biten).
- Konum çağrısı (`requestLocation`) **yalnız tap akışı dosyasında**; bir çağrı-yeri izin listesi testi bunu pinler
  (M16-02 denetimi: periyodik bir `requestLocation` takibe dönüşür, R2'nin periyodik ağı yalnız aynı dosyayı görür).

**Tuzaklar.** UI zamanlayıcıları (`Timer.publish(every:)`, `Timer.scheduledTimer(… repeats: true)`) tap akışındaki
`requestLocation` ile **aynı dosyada** olursa `redline-check.sh` R2'nin periyodik ağı FAIL verir (M16-02: dosya bazlı
ağ) — zamanlayıcılar ayrı bir görünüm dosyasında yaşar.

> **Logo notu (M16-07 incelemesi, 2026-11-14).** Uygulamaya logo baytlarının yolu bu kartla (ya da eşiyle) eklenince
> **eş işletme plaketi** ayrımı gerekir: tarayıcıda `tenant.PageBrand.PeerTag` doluysa logo `/t/logo/<sha>?tag=<uid>`
> (eşin logosu, `holding_logo` üzerinden) yolundan gelir, değilse oturumun kendi işletmesinin yolundan. `tap/start`'ın
> `brand`'i bugün bu ayrımı taşımaz; logo yolu eklenirken taşımalıdır.

> **M16-15 devirleri (denetim, 2026-11-18).** (1) Dağıtım hedefi iOS 15.0; iPhone 6s/6s Plus/SE (1. nesil) iOS 15
> çalıştırır ama Core NFC okuyamaz ve `UIRequiredDeviceCapabilities`'te `nfc` **bilerek yok** (Faz D canlı QR onlara
> açık) → okuma başlatmadan önce `NFCNDEFReaderSession.readingAvailable` kontrolü, yoksa QR/tarayıcı yolu. (2) Wordmark
> bugün sistem fontu (`RootView.swift:12,15`); tap ekranı Space Grotesk + IBM Plex Mono'yu gömer (skill `tappa-brand`).
> (3) Yalnız `NDEF` içeren NFC yetki listesinin App Store yüklemesinde ITMS-90778 verdiği raporlanıyor — **ölçülmedi**;
> M16-18'in App Store Connect'e ilk beta yüklemesi ölçer.

> **M16-16 devirleri (yeniden denetim, 2026-11-18).** (1) **Tap ekranı dokunuştan önce kişinin adını gösterir** — ADR
> 0045 kabul edilmiş riskin hafifletmesi (kullanıcı kararı 2026-11-18). (2) Kurulu ama aktive edilmemiş uygulama plaket
> linklerini yutar (`ActivationModel.swift` tap adımı dışında yok sayar) — bu kart yönlendirmeyi kurar. (3) `save()` sil +
> ekle atomik değil — `SecItemUpdate`. (4) Saati 24 saatten fazla kaymış telefon gerçek ölçümü hiç almaz ve hep *signed
> out* görür; kullanıcıya *"telefonunun saatini kontrol et"* demeli. `ServerClock` yorumundaki *"UTC"* örneği yanlış
> (saat dilimi mutlak zamanı değiştirmez). (5) `complete` yanıtı yolda kaybolursa sunucuda cihaz etkin, telefonda kayıt
> yok → M16-18 cihaz turunda ölçülür.

## M16-18 — Faz C kapanışı — gerçek cihaz turu

- **Bağımlılık:** M16-17
- **Araç:** yapıcı (kullanıcıyla) + üçüncü göz · **Mercek: iki**
- **Commit:** `docs(ios): record the device tour`

**Amaç.** iOS MVP'sini iki iPhone sınıfında gerçek cihazla ölçmek ve fazın büyük testlerini bir kez koşmak.

**Kabul kriterleri.**
- iPhone XS+ **ve** iPhone 7–X üzerinde M16-14'ün senaryo listesi (sahte konum yerine: konum izni reddi ve *"Precise
  Location"* kapalı); kanıt kuralı M16-14'ünkü: kayıt yazan adım DB satırıyla, kayıt yazmayan adım (telefon çıkışı
  sonrası) `transactions` sayısının değişmediğiyle; deaktivasyon adımı tek kullanımlık test çalışanıyla.
- Q11 için yan ölçüm: aynı iPhone'da tarayıcı oturumu ile uygulama cihazının bir hafta sonraki durumu (bilgi, kabul
  kriteri değil).
- Fazın büyük testleri bir kez; *doğrula* sonuçları ADR 0045'e not.

> **M16-17 devirleri — cihaz turunda ölç (2026-11-18).** (1) XS+ arka plan okuması → Universal Link → uygulama; öne
> dönüş/link yarışı gerçek UIKit sırasıyla. (2) `UIApplication.open` uygulamanın kendi Universal Link'ini Safari'de mi açıyor
> (döngü riski; kayıt yazılmaz). (3) İlk basışta konum izni uyarısı + NFC sayfası aynı anda (7–X). (4) `isSimulatedBySoftware`
> gerçek cihazda. (5) iOS 15'te woff2 fontları. (6) ITMS-90778 (yalnız-NDEF yetki listesi) ilk beta yüklemesinde. (7)
> `complete` yanıtı kaybı (cihaz sunucuda etkin, telefonda kayıt yok). (8) Bekletilen bildirimden harcanmış URL → reject +
> debounce sıklığı. (9) DÜŞÜK UX: başarısız aktivasyon sonrası kuyruktaki link *not set up*'a düşer; `complete` sırasında
> kuyruğa giren aynı URL tap ekranını açar; basış sırasında link gelirse `resultUnseen` kalır. (10) DB'de `sun_valid=true`.

---

# Faz D — NFC'siz telefonlar

## M16-19 — Şema: canlı QR ekranları ve `qr_live` kanal envanteri

- **Bağımlılık:** M16-10 · Q38, Q39 · Q43 cevaplandı (2026-11-14: holding `qr_live` kapalı, site sütunları dolu)
- **Kırmızı çizgi:** §4.3, §4.5, §4.6, §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): add live QR displays and the qr_live channel`

**Amaç.** Kanal sözlüğünü **tek seferde ve eksiksiz** genişletmek: bir CHECK'i ya da tetikleyiciyi unutmak ya kaydı
reddeder (§4.6) ya da akıştan sessizce düşürür.

**Kabul kriterleri.**
- `live_qr_displays`: `tenant_id`, `location_id`, eşleşme (tek kullanımlık kod özeti + süresi), ekran oturumunun
  özeti, `created_at`, `revoked_at` (tek yönlü); §6 beşlisi + GRANT + RLS izolasyon testi.
- **Tenant bilinmeden çözümleme** (M16-03 ile aynı kural): ekranın eşleşme kodunu girdiği istek ve ekran oturumu
  çerezi tenant bilinmeden gelir; ikisinin çözümü de `00003`'ün `resolve_session_by_token_hash` / `tappa_resolver`
  desenindeki dar SECURITY DEFINER fonksiyonlarıdır (en çok 1 satır, sütun GRANT'ı, `SET search_path = pg_catalog,
  pg_temp`, EXECUTE yalnız `tappa_app`); eşleşme kodunun tüketimi tek ifadedir.
- **Üçüncü çözümleyici — dokunuş anı.** Çalışanın okuttuğu token'dan ekran satırına (tenant, lokasyon, `revoked_at`,
  lokasyonun durumu) giden çözüm de tenant bağlamı olmadan yapılır ve holding durumunda (ADR 0027 — KM çalışanı KF
  ekranında) **başka bir tenant'ın** satırını okumak zorundadır; §5 satır 1'in eşi (M16-21) buna dayanır. Bu,
  `00004`'ün `resolve_tag_by_uid`'inin (`00004_create_tags.sql:143`, sahibi `tappa_resolver` `:178`, EXECUTE yalnız
  `tappa_app` `:182-183`) birebir eşidir: ekran kimliği → (tenant, lokasyon, iptal durumu, lokasyon durumu), en çok 1
  satır, sabit `search_path`. *İmzalı token tenant'ı zaten taşıyor, arama normal tenant kapsamında kalabilir*
  seçeneği **alınmadı**: token iptali göremez (iptal edilmiş ekranın son token'ı ~20 sn yaşar ve iptal ekranın
  satırındadır), ve holding dokunuşunda oturumun tenant'ı token'ınkinden farklıdır — normal kapsam satırı göremez.
- Üç çözümleyici için RLS testi (fonksiyonsuz `tappa_app` başka tenant'ın ekranını göremez) + definer yüzeyi envanteri
  (`TestResolverAccess_NoSqlcQueryNamesADefiner`, `TestResolverAccess_TheCallSitesAreTheOnesInventoried`) güncellenir.
- **Envanter tablosu bir başlangıç listesidir, eksiksizlik kanıtı değildir.** Üç denetim turunda üç kez yeni bir
  biçim bulundu; bu yüzden eksiksizliğin kanıtı bir arama deseni değil **M16-21'in parite testidir** (aşağıda). Liste
  kartın içine yazılır: SQL'de `'nfc', 'qr'` / `channel IN` / `NOT IN` / `<> 'manual'` / `!= 'manual'`; Go'da
  `== Channel(NFC|QR)`, `!= Channel(NFC|QR)`, `case …ChannelNFC` / `case …ChannelQR`, `!= ChannelManual`,
  `== ChannelManual` — geçtiği migration fonksiyonu, CHECK, sorgu ve Go dosyası. Pozitif Go karşılaştırmalarından
  bugün bilinenler (her biri `qr_live`'ı **dışarıda bırakır**): `internal/domain/checkin/checkin.go:1751`
  (`BusinessDayStart` — iş günü), `:1869` (`shiftColumns` — vardiya anlık görüntüsü), `:1899`
  (`directionSourceColumn`), `:1688` (`feedEligible`), `:1815` (NFC-only), `clientColumn` (M16-04 —
`transactions.client`; **`qr_live` de client alır — onu uygulama ya da tarayıcı yazar**, ve 00109'un
`transactions_manual_has_no_client` CHECK'i yalnız `manual`'a bakar); `internal/domain/tap/directionsource.go:43`
  (`RecordedDirectionSource`); `internal/handler/tapcontext.go:388` — imzalı tap bağlamını `nfc`/`qr` dışında
  `errTapContextMalformed` ile **reddeder** (M16-07 bu bağlamı yeniden kullanırsa kayıt kaybı, §4.6);
  `internal/handler/claimqueue.go:615` (`claimChannelWord`); `internal/handler/tap.go:645` (önizleme kanalı);
  `internal/sandbox/simulate.go:118` (API kanal alanı), `:353` (`InFeed`); `internal/policy/guardrails.go:396`,
  `:479`, `:586`. (ters biçim `qr_live`'ı **sessizce kabul eder** — bu
  doğru olabilir, ama her biri için nedeni yazılır: ör. `db/queries/daycutoff.sql:73`, `00068:336`,
  `internal/domain/checkin/checkin.go:1777` (`SunValid` sütunu), `:1796` (site yazımı — `tagRow` varsayar, `qr_live`'da
  plaket satırı yoktur), ve `:1154` aşağıda) (başlangıç: ADR 0045 Bedel 6'nın listesi) — her biri için *"qr_live girer /
  girmez, neden"*. `transactions_site_by_channel` (`00043:139-142`) için üçüncü kol (ekranın lokasyonu dolu, plaket
  uid'i boş — öneri). Envanter en az şunları adlandırır: `00005`, `00043`, `00044`, `00046`, `00048`, `00049`, `00050`,
  `00067`, `00068`, `00095`; `db/queries/transactions.sql` — **`SecondsSinceLastRecordedTap`'in `channel IN ('nfc',
  'qr')` filtresi (`:317`) dahil**: debounce'un sunucu saati bacağıdır (ADR 0006); `qr_live` oraya girmezse geriye
  tarihlenmiş bir `qr_live` dokunuşu debounce'u sessizce atlar — ve `daycutoff.sql`.
- **Holding ret satırı (Q43 kapalı, 2026-11-14):** `site_punches.channel` CHECK'i (`00046:137`) `qr_live`'ı kabul
  edecek şekilde genişler ve `transactions_site_by_channel` (`00043:139-142`) `qr_live` için ret satırının site kolunu
  (`site_location_id` dolu, `site_tag_uid` NULL) kabul eder.
  Site tarafı ayıklaması: `site_punches_other_refusal_hourly` (`00046:168-170`, `NULLS NOT DISTINCT`) `site_tag_uid`'i anahtar
  alır; `qr_live` ret satırında NULL olduğu için bir çalışanın aynı saatte site tenant'ının **bütün** canlı QR ekranlarındaki
  retleri tek `refused` satırına iner (plaketli retlerde plaket başına). Kart ya `site_location_id`'yi anahtara ekler ya da
  farkı kabul edilen risk olarak yazar; parite/DB testi aynı saatteki ikinci reddin `ON CONFLICT DO NOTHING`'e düştüğünü bilir.
- Akış tetikleyicisinin gövdesi (`00067`'nin `punch_feed` fonksiyonu) ve Go aynası `feedEligible` **aynı commit'te**
  Q38'in cevabına göre; parite testi.
- `Down`: `qr_live` satırı varsa reddeder.

## M16-20 — Canlı QR ekran sayfası ve panelden eşleme

- **Bağımlılık:** M16-19
- **Kırmızı çizgi:** §4.5, §4.7, §9 (panel)
- **Araç:** yapıcı + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(liveqr): pair a venue screen and rotate its code`

**Amaç.** Mekândaki herhangi bir tabletin, uygulamasız, panelden bir lokasyona eşlenip ~20 sn'de bir dönen imzalı QR
göstermesi.

**Kabul kriterleri.**
- **Eşleşme kodunun gücü:** kod ya en az **64 bit** rastgeledir, ya da kısa bir kodsa (yazılabilir olsun diye)
  **IP başına ve tenant başına deneme sınırı** taşır — `internal/httpx/ratelimit.go`'da kendi limiter'ı — ve her
  başarısız deneme `audit_log`'a yazılır (kod değeri yazılmadan, CLAUDE.md §7). Seçenek: ekran oturumunu lokasyonun
  IP aralığına bağlamak (ADR 0045 Bedel 5) — kart ölçer, gerekçesiyle seçer.
- Panel: lokasyon sayfasında *"Add a live QR screen"* → tek kullanımlık eşleşme kodu (kısa ömürlü); ekranlar listesi
  ve iptal; her eşleme ve iptal `audit_log`.
- Ekran sayfası (Go ikilisinden, uygulama gerekmez): kodu girer → ekran oturumu (HttpOnly çerez, DB'de özet) → QR
  gösterir; QR ~20 sn'de bir döner (sayfa sunucudan yeni token çeker; Node yok, el yazımı küçük JS ya da HTMX).
- Token sunucu sırrıyla imzalı (öneri: yeni env sırrı; yoksa canlı QR kapalı, panic yok); token lokasyonu ve zaman
  penceresini bağlar; QR'ın URL'si uygulama tarafından da yakalanabilir yol altında (M16-08).
- Ekran kişisel veri göstermez; ekran sayfasının CSP'si tap ekranınınki kadar dar.
- Ekran çevrimdışıyken eski kodu göstermeye devam etmez (son başarılı yenilemenin üstünden süre geçince QR gizlenir).
- İmza anahtarı env'i `internal/config`'in `namedKeys()` çift-çift farklılık kapısına girer (`config.go:1214`; aynı
  değer başka bir anahtarla paylaşılırsa açılış reddeder).
- **Log testleri:** hiçbir log satırı, hata mesajı ve `audit_log` `detail`'i canlı QR token'ını, ekran eşleşme kodunu,
  ekran oturumu çerezini ya da imza anahtarını taşımaz (CLAUDE.md §7, M16-01'de genişleyen liste).
- `/.well-known` iki dosyasının yol listesine canlı QR yolu eklenir (M16-08'in Faz D maddesi) — uygulama kuruluysa
  canlı QR'ı da yakalar.

## M16-21 — `qr_live` karar motorunda

- **Bağımlılık:** M16-20 · Q43 cevaplandı (2026-11-14: holding `qr_live` kapalı, site sütunları dolu)
- **Kırmızı çizgi:** §5 (normatif), §4.6, CLAUDE.md §8
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(tap): accept a live QR as a channel`

**Amaç.** Canlı QR dokunuşunu karar motoruna plaketsiz bir tap kanalı olarak sokmak; plaket varsayan her yeri (§5 satır 1,
tenant çözümü, holding, debounce, `/v1`) açık bir kuralla karşılamak.

**Kabul kriterleri.**
- `sun.Channel` / `tap.Channel` sözlüğüne `qr_live`; kanal **token doğrulamasından** türetilir, istek alanından değil.
- Token doğrulaması sabit zamanlı karşılaştırma; süre dolmuş / imzası bozuk token Q39'un cevabına göre (kayıt kuralı
  §4.6 ile).
- `sys:tap-freshness`'in `qr_live`'a uygulanıp uygulanmayacağı ölçülerek karar verilir (bugün NFC-only,
  `guardrails.go:479`) ve gerekçesi koda yazılır.
- **Plaketsiz tap.** `tap.Input.Tag` bugün *"Always present for a resolved tap"*tir (`internal/domain/tap/types.go:471-473`);
  `qr_live` satırının plaketi yoktur. Kart şunları açık kuralla karşılar (her biri bir `TestDecide_` vakası ya da DB
  testi):
  - **§5 satır 1'in eşi:** iptal edilmiş (`revoked_at` dolu) ya da lokasyonu pasif bir ekranın token'ı → sonuç
    (öneri: `reject`, satır 1 gibi) **ve kaydın yazılıp yazılmadığı** (öneri: kimliği çözülmüş kişi için yazılır,
    §4.6).
  - **Lokasyon ve tenant token'dan türetilir** — tap yolunda `resolve_tag_by_uid`'in (`00004`) yerini M16-19'un
    üçüncü çözümleyicisi alır; istekten lokasyon alanı okunmaz.
  - **İstek doğrulaması plaket ister:** `internal/domain/checkin/checkin.go:1154`
    (`r.Channel != tap.ChannelManual && r.TagUID == ""` → `ErrInvalidRequest`, *"a tap needs a tag"*) bugün plaketsiz
    her `qr_live` isteğini reddeder — kayıt kaybolur (§4.6). Kural `qr_live`'ı ekran kimliğiyle karşılar; `:1796`'nın
    site yazımı (`tagRow`) da öyle. **İki DB testi**, çünkü site sütunları yalnız **başka tenant** için doludur
    (`00043:133-136`: `transactions_site_needs_its_tenant`, `transactions_site_is_another_tenant`;
    `checkin.go:1796-1813` site sütunlarını yalnız plaketin tenant'ı oturumunkinden farklıyken yazar):
    - **yerel:** `qr_live` + IP → satır yazılır, verdict `ok`, `location_id` = ekranın lokasyonu, `tag_uid` NULL,
      `site_tenant_id` / `site_location_id` / `site_tag_uid` NULL;
    - **holding** (Q43: kapalı — ret, kayıt yazılır): verdict `reject`, `site_tenant_id` = ekranın tenant'ı,
      `site_location_id` dolu, `site_tag_uid` NULL, `location_id` / `tag_uid` NULL — `transactions_site_by_channel`'ın (`00043:139-142`) M16-19'da eklenen
      `qr_live` kolu ve M16-19'un üçüncü çözümleyicisi.
  - **Token tenant'ı ↔ oturum tenant'ı:** başka (holding dışı) tenant'ın canlı QR'ı → `sys:tenant-mismatch`'in eşi (yönlendirme, kayıt yok); holding
    durumu (ADR 0027 — KM çalışanı KF ekranında) **kapalıdır** (Q43, kullanıcı kararı 2026-11-14): dokunuş
    reddedilir, kaydı yazılır (§4.6), ret satırı site sütunlarını taşır ve site tenant'ı onu `site_punches`'ta
    `refused` olarak görür (K-1h); retin sid'ini kart adlandırır (bir guardrail; `sys:tenant-mismatch` değil — o bir
    yönlendirmedir ve kayıt yazmaz). ADR 0027 değişmez. Site sütunları (`site_tag_uid` NULL, `site_location_id` dolu)
    M16-19'un üçüncü koluyla uyumlu. *İleride açmak kullanıcı onayı + ADR 0027 §2 değişikliği ister.*
  - **`/v1` `tagId`:** `qr_live` olayında `null` (şema zaten `["string", "null"]`, `openapi.yaml:1130-1133`; açıklaması
    *"also for a QR punch"* der) — anlamı açıklamaya yazılır (Q38).
- `TestDecide_` önekli vakalar: `qr_live` + IP → `ok` · `qr_live` + GPS-only → `ok` (`base:gps-only-allow`) ·
  `qr_live` + hiçbiri → `flag` · aynı kişi 60 sn içinde → `ignored` · **kanallar arası:** `nfc` dokunuşundan 60 sn
  içinde `qr_live` → `ignored` (ve tersi) · **geriye tarihlenmiş** `occurred_at`'li `qr_live` öncülü 60 sn içinde
  yazılmışsa → `ignored` (sunucu saati bacağı, `SecondsSinceLastRecordedTap` — DB testi) · farklı iki kişi aynı
  token'la art arda → ikisi de kayıt · **holding** `qr_live` (Q43 kapalı) → `reject` + kartın adlandırdığı guardrail · statik QR'ın sonuçları **aynen**.
- **Log testi:** token, imza ve doğrulama hatasının metni log'a ve hata gövdesine girmez (CLAUDE.md §7).
- Yeni baseline belgesi gerekip gerekmediği bu vakalarla ölçülür (ADR 0045 §2.5: büyük olasılıkla gerekmez); gerekirse
  `BaselineVersion` + duyuru.
- Rapor, liste, anomali ekranı ve `/v1` `qr_live`'ı tap kanalı olarak sayar (M16-19 envanterinin Go yarısı).
- **Eksiksizliğin kanıtı — parite matrisi (mekanizma, desen değil).** Gerçek yoldan uçtan uca DB testleri: her
  hücrede bir `qr` ve bir `qr_live` dokunuşu imzalı tap bağlamı → `checkin` → satır yolundan geçirilir.
  - **Amaç:** `qr_live`, aynı kanıtla `qr`'nin yazdığı her şeyi yazar — `transactions`, `punch_events`; holding'de (Q43
    kapalı) yazdığı şey ret satırı ve `site_punches`'taki `refused` satırıdır; bir şeyi eksik yazarsa test kırmızıdır ve envanterde adı geçmeyen bir yer de böyle yakalanır.
  - **Yalıtım:** iki kanal aynı çalışanla art arda koşmaz (ikinci dokunuş debounce'a düşer ya da yönü çevirir);
    **varsayılan, birebir aynı fikstürde klonlanmış çalışandır**. Gerekçe: gerçek yol commit eder; dış transaction
    yolu (`checkin.Database` tüketici tarafı arayüzdür, `checkin.go:141-144`) **ayrı bir test bağdaştırıcısı** ister —
    sandbox'ınki (`onRunner`, `internal/sandbox/runner.go:96-107`) dışa açık değildir ve `errAnotherBusiness` bir sandbox
    kuralıdır, mekanizmanın sınırı değil.
  - **Matris {yerel, holding} × {`ok` dokunuş, `flag` → müdür onayı}** — akış tetikleyicilerinin `qr_live`'ı bir
    `NOT IN` / `IN ('nfc', 'qr')` süzgeciyle sessizce atlayan dört kolu: `00067:193` (yerel `ok`), `:234` (yerel
    onay), `:366` (holding `ok`), `:408` (holding onayı). Holding hücreleri **parite değil** şunu ölçer
    (Q43 kapalı, 2026-11-14) — holding kuralı statik `qr`'yi hiç reddetmez (K-1g,
    `sys:cross-tenant-qr-site-ip`: IP ile `ok`, IP'siz `flag`), bu yüzden bu dalda `qr` hücresi referans değildir:
    `qr_live` reddi bir `transactions` satırı yazar (§4.6) · `punch_events`'e olay girmez · `site_punches` satırı
    `outcome='refused'` ile **CHECK hatasız** yazılır (kapalı küme `00046:139-140`; bugünkü eşleme
    `holding_feed`'de, `00067:300`, `:357-363`; `site_punches.channel` CHECK'i `00046:137` M16-19'da genişlemiş olmalı).
  - **Bilinen yapısal farklar** (eşitlik beklenmez): kanal sütunu ve kanal eşlemesi; plaket sütunları; kimlik, sıra ve
    zaman damgası sütunları (`site_punches.bucket_hour` dahil — `now()`'ın saatidir; ya dışlanır ya iki koşu aynı saate
    sabitlenir); klonlanmış çalışanın kimlik sütunları; `policy_context`'te `tap:channel` (`decide.go:93`) ve zamandan
    türeyen `tap:pageAgeSeconds` (`decide.go:107-108`) — ya `checkin`'in enjekte edilebilir `now`'ı
    (`checkin.go:308-316`) ve bağlamın `IssuedAt`'i iki koşuda sabitlenir ya da anahtar dışlanır. Plaket
    anahtarları `tag:status`, `tag:locationId`, `location:id` yalnız `in.Tag != nil` iken yazılır (`decide.go:115-118`):
    `qr_live`'daki ekran karşılıklarını M16-21 açık bir kuralla tanımlar ve test o kurala göre karşılaştırır.
    `policy_context`'in geri kalan anahtarları (`tap:ipMatch`, `tap:gpsMatch` dahil) eşittir.
  - **Kesin sütun ve anahtar listesini yapıcı**, testi yazarken şemaya karşı sabitler; liste M16-21'in kendi
    denetiminde incelenir. Bu kart onu önceden saymaz.
  - Debounce davranışı bu matrisin değil yukarıdaki `TestDecide_` / DB vakalarının (kanallar arası, geriye
    tarihlenmiş) işidir.
- Parite testinin ulaşamadığı yerler (panel metni `claimChannelWord`, sandbox simülatörü gibi kayıt yazmayanlar) tek
  satırlık bir takip maddesi olarak yazılır — yeni arama desenleri eklenmez.
- CLAUDE.md §5 *"QR kanalı"*na canlı QR cümleleri (bu kartla). ADR 0005'e canlı QR aktarma riski (Bedel 5) —
  `TestADR0005_TheRiskCountMatchesTheTable` yeşil.

## M16-22 — Uygulamada QR okuma (statik + canlı)

- **Bağımlılık:** M16-21, M16-13, M16-17 · Q36 (c)
- **Kırmızı çizgi:** §9, §1
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `feat(mobile): read plaque and live QR codes`

**Amaç.** NFC'siz telefonda uygulamaya statik ve canlı QR'dan girilebilmesi.

**Kabul kriterleri.**
- Öneri (Q36 c): MVP'de uygulama içi tarayıcı **yok** — sistem kamerası QR'ı okur, App Link / Universal Link uygulamayı
  açar. Q36 başka seçenek seçerse: iOS AVFoundation (platform), Android yalnız onaylanan kütüphane.
- NFC'siz telefonda tap ekranı aynı; giriş noktası QR.
- Kamera izni yalnız uygulama içi tarayıcı seçilirse istenir.

## M16-23 — NFC'siz aktivasyon: canlı QR ve müdür onayı

- **Bağımlılık:** M16-21, M16-22 · uygulama yolu için M16-12, M16-16
- **Kırmızı çizgi:** §4.5, §4.6, §5 (aktivasyon), §9 (aktivasyon ailesi)
- **Araç:** yapıcı + agent `tappa-db-migrator` (bekleyen istek tablosu) · **Mercek: iki**
- **Commit:** `feat(activate): activate a phone that cannot tap`

**Amaç.** NFC'siz telefonu aktive etmek: lokasyonda canlı QR ekranı varsa onunla, yoksa müdür onayıyla (ADR 0026 Bedel 3'ün
kalkışı).

**Kabul kriterleri.**
- **Canlı QR yolu:** onaylı (bağlı) aktivasyonu olan tarayıcı ya da uygulama, davetle **aynı tenant'taki** aktif bir
  ekranın geçerli token'ını okur → `CompleteByTap`'in sırası, `sun.Verify` yerine token doğrulaması; `transactions`
  satırı yok.
- **Kapı (kullanıcı kararı):** müdür onayı yolu **yalnız** davetin lokasyonunda (öneri: çalışanın profil şubesi ya da
  tenant'ın hiçbir lokasyonunda — kart ölçer ve seçer) aktif canlı QR ekranı **yoksa** sunulur; ekran varsa sihirbaz
  canlı QR'ı ister. Test: ekranlı lokasyonda müdür onayı isteği açılamaz (sunucu reddeder, yalnız gizlenmez).
- **Bekleyen istek tablosu** §6 beşlisiyle (`tenant_id`, ENABLE + FORCE RLS, politika, indeks) + en dar GRANT + RLS
  izolasyon testi (M16-03'teki gibi); onay/ret tek yönlü.
- **Müdür onayı yolu:** sihirbazda *"My phone can't tap"* → bekleyen istek (eşleşme kodu telefonda ve panelde) →
  müdür onaylar ya da reddeder → `audit_log` (kim, ne zaman, hangi çalışan; kod ve davet kodu log'a ve `detail`'e
  yazılmaz) → telefon bir sonraki durum sorgusunda oturumu/cihazı alır. Onay süreli; süresi dolan istek düşer.
- Kendi kendine onay: müdür kendi telefonunu onaylayamaz (ADR 0037'nin ruhu — ölç, uygulanabiliyorsa).
- Sihirbaz metinleri §9 aktivasyon ailesi kararı ister — kullanıcıya gösterilir.
- ADR 0026'ya not: Bedel 3 iptal edildi (silinmez). CLAUDE.md §5 *"Aktivasyon dokunuşu"*na NFC'siz yollar.
  **§5 sıra notu:** CLAUDE.md §5 *"Aktivasyon dokunuşu"* bugün sırayı *"`sun.Verify` → aktif plaket → tüketim"*
  diye yazar (`CLAUDE.md:391`); ADR 0026 md. 3 ve plan ise *"ilerletmeyen ön kontrol (aynı tenant, aktif) → `sun.Verify`
  → tüketim"* der, ve `internal/handler/activate.go:852-862`'nin ORDER yorumu eski sırayı (`sun.Verify` → plaket
  denetimleri) taşır. Cümleyi yazan kart sırayı kodun **ölçülen** sırasına eşitler; yorum ile metin çelişiyorsa
  ikisi aynı commit'te düzeltilir.

## M16-24 — Faz D kapanışı

- **Bağımlılık:** M16-19 … M16-23
- **Araç:** yapıcı (kullanıcıyla) + üçüncü göz · **Mercek: iki**
- **Commit:** `docs(liveqr): record the venue screen tour`

**Amaç.** Faz D'yi gerçek bir ekran ve NFC'siz bir telefonla ölçmek ve fazın büyük testlerini bir kez koşmak.

**Kabul kriterleri.**
- Gerçek bir tablette canlı QR ekranı + NFC'siz telefon: tarayıcıyla ve uygulamayla aktivasyon (iki yol), giriş,
  çıkış, 20 sn'den eski fotoğrafla deneme (Q39'un sonucu), ekran iptali sonrası ret; her biri DB satırıyla.
- `/v1` akışında `qr_live` dokunuşunun görünüşü Q38'e uygun (sandbox ya da yerel).
- Fazın büyük testleri bir kez.

---

# Faz E — Kanıt + raporlar

## M16-25 — Attestation — yalnız kanıt

- **Bağımlılık:** M16-14, M16-18 · Q37
- **Kırmızı çizgi:** §4.1 (attestation biyometri değildir — ama cihazdan veri alır; ne alındığı yazılır), §4.3, §4.7
- **Araç:** yapıcı + agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(appdevice): record device attestation as evidence`

**Amaç.** App Attest ve Play Integrity sonucunu kararı etkilemeden kanıt olarak kaydetmek.

**Kabul kriterleri.**
- iOS App Attest (aktivasyonda attestation, tap'te assertion) ve Android Play Integrity; Go tarafı Q37'nin cevabıyla
  (öneri: yerel doğrulama, yeni bağımlılık yok ya da en küçüğü).
- Kayıt yeri bir sütunsa yalnız INSERT ve NULL kabul eden CHECK (M16-04'ün kuralı); bir tabloysa §6 beşlisi + en dar
  GRANT + RLS izolasyon testi (M16-03'teki gibi).
- Sonuç kaydedilir (öneri: `device_attested`); **`policy_context`'e girmez**, güven puanını ve verdict'i değiştirmez
  (`TestDecide_` regresyonu: aynı girdiler, attestation var/yok → aynı karar).
- Apple/Google'a giden her çağrı gizlilik bildirimine yazılır (M16-27).
- CLAUDE.md §7 *"Asla loglanmaz"* listesi genişler: Play Integrity çözme/doğrulama anahtarları (Q37 (b) seçilirse),
  ham Play Integrity token'ı, App Attest attestation ve assertion nesneleri, cihaz açık anahtarı ve parmak izi; anahtar
  env'leri `internal/config`'in `namedKeys()` kapısına girer; log testi bunları hiçbir log satırında ve `audit_log`
  `detail`'inde bulamaz.
- Attestation başarısızlığı dokunuşu bloklamaz (§4.6).

## M16-26 — Raporlar ve panelde cihaz listesi; Faz E kapanışı

- **Bağımlılık:** M16-25, M16-24 (`qr_live` oranı Faz D'nin kanalını ister)
- **Kırmızı çizgi:** §4.5 (tenant panelinde `app_devices` okuması + yazma yolu)
- **Araç:** yapıcı + skill `tappa-brand` · **Mercek: iki** (tek cihaz iptali panelin yazma yoludur)
- **Commit:** `feat(dashboard): show app, attested and mock-location ratios`

**Amaç.** Müdüre uygulama/tarayıcı, attested, sahte konum ve canlı QR oranlarını ve çalışan başına cihaz listesini göstermek;
tek cihazı iptal edebilmek.

**Kabul kriterleri.**
- M6-11 anomali raporuna oranlar: uygulama / tarayıcı · attested · sahte konum bildirimi · `qr_live` — çalışan ve
  lokasyon kırılımında; eksik veri (`client` NULL tarihsel satır) ayrı ve açıkça.
- Çalışan kartında cihaz listesi (platform, kaba etiket, ilk/son kullanım, attested) ve tek cihaz iptali.
- Tek cihaz iptali **yeni bir yazma yoludur** (panelde tarayıcı için tek oturum iptali yoktur) ve T59'un reddini
  taşır: **deaktive bir kişinin cihazı iptal edilemez** — `tenant.ErrSignOutDeactivated`'ın
  (`internal/domain/tenant/staffsignout.go:55-57`, `:103`) ikizi; aksi hâlde kişinin sonraki dokunuşları satır 3'ün
  kayıtsız yoluna düşer (2026-08-07 kararının önlediği kayıp, §4.6). DB testi: deaktive kişide deneme reddedilir ve
  sonra `revoked_at IS NULL`.
- Tek cihaz iptali **tek yönlüdür** (`00099` deseni), `audit_log` yazar, başka tenant'ın cihaz kimliğiyle istek → 404
  (RLS + açık `tenant_id` filtresi; DB testi); yetki policy motorundan (`base:authz-*`), CSRF panelin mevcut kapısıyla.
- Fazın büyük testleri bir kez.

> **Bilinen sıra (M16-07 güvenlik incelemesi, DÜŞÜK, 2026-11-14).** Kişinin canlı oturumu yok, yalnız cihaz D1 var; bir
> uygulama aktivasyonu uçuşta (D1'i iptal edip D2'yi doğuracak). Aynı anda müdürün *"telefonlarını çıkar"*ı koşarsa ve
> aktivasyonun transaction'ından **önce** biterse 0 satır görür — ekran *"no phones signed in"* der — ama aktivasyon
> commit edince D2 yaşar. Kilit sırası aynı (oturumlar → cihazlar → nonce'lar), deadlock yok; bu bir **sıra** sonucudur.
> Seçenek: panel 0 sonucunda bir kez yeniden dener, ya da metin *"şu an"* der. Cihaz listesi (bu kart) D2'yi gösterir.

---

# Faz G — Personel self-servis

> **Neden bu faz (kullanıcı kararları 2026-11-18, yukarıda).** Uygulamayı mağazaya hazır ve dokunuştan öte yararlı
> yapmak: işletmenin yönettiği Saatlerim, ayarlar/gizlilik ve uygulama içi hesap silme talebi (Apple 5.1.1(v)), işverenin
> açtığı hesaba e-postayla giriş (ADR 0049), ve inceleyicinin plaketsiz geçebileceği bir App Review yolu (2.1(a)).
> **Yayından (Faz F) önce** gelir.
>
> **Uygulama sırası** (numara sırası değil): M16-30 → M16-31 → M16-32 → M16-33 → M16-34 → M16-35 → M16-36 → M16-37 →
> **M16-41** → M16-38 → M16-39 → M16-40. M16-41 (gizlilik bildiriminin yayımı) denetim turunda eklendi ve M16-38'den
> **önce** gelir; numarası bu yüzden sondadır.
>
> **Ortak kurallar.** (1) Yeni her çalışan yüzeyi tap ekranından **ayrıdır**; tap ekranı tek ekran, tek düğme kalır
> (CLAUDE.md §9) — plaket linki, NFC okuması ve QR açılışı bugünkü gibi doğrudan tap ekranını açar. (2) Yeni her çalışan
> ekranı ve panel kontrolü **kurulmadan önce** kullanıcıya AskUserQuestion + mockup ile sorulur; skill `tappa-brand`.
> (3) Sunucu her uçtan önce sevk edilir (M16-07 sürümleme kuralı). İki uygulama yanıtları **toleranslı** okur — bilinmeyen
> yanıt alanı yok sayılır (ölçüldü: Android `mobile/android/app/src/main/kotlin/mt/taptime/app/wire/Wire.kt:7-9`; iOS
> `Decodable` yapıları `mobile/ios/Taptime/AppClient.swift:22` — Swift `Decodable` bilinmeyen anahtarı varsayılan olarak
> yok sayar), dolayısıyla yeni yanıt alanı eski uygulamayı kırmaz. (4) Büyük testler yalnız M16-40'ta. (5) **Test adı
> kuralı:** yeni testler `[G<kart>-<harf>]` kimliğiyle **tarif edilir**; Go adını yapıcı koyar ve kimliği testin
> yorumuna yazar (`TestEveryNamedTestExists` var olmayan `Test…` adını reddeder). Denetçi kimliği `git grep`'le bulur.
> (6) **Ortam:** Faz G'nin hiçbir adımı KF/KM'de **yazmaz**; gerçek cihaz ve e-posta ölçümleri yalnız üretimdeki
> *"Taptime Demo"* işletmesinde (M16-34'ün kullanıcı adımı) ya da yerel/şerit veritabanında yapılır.
> **Atıf kuralı:** `file:line` atıfları `origin/main` `6ab423eb`'ye göredir; yapıcı kendi turunda yeniden okur.

## M16-30 — Saatlerim ayarı: işletmenin aç/kapa ve görünürlük penceresi (şema + panel)

- **Bağımlılık:** M16-10 · Q48 cevaplandı (a) (2026-11-19) · kullanıcının §9 kararı (panel kontrolü)
- **Kırmızı çizgi:** §4.5 (owner eylemi), §6 (yeni sütun: sütun düzeyi GRANT, `Down` dolu)
- **Araç:** yapıcı + agent `tappa-db-migrator` (00115) + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(account): let the owner choose whether staff see their hours`

**Amaç.** Kullanıcı kararı 4 (2026-11-18): Saatlerim'i **işletme yönetir** — owner aç/kapa ve geriye görünürlük
penceresi seçer; sunucu zorlar.

**Kabul kriterleri.**
- **Yer: politika motoru değil, düz bir işletme ayarı.** `internal/policy` dokunuş kararına *effect* döndürür (CLAUDE.md
  §3); bu ayar hiçbir verdict'i, güveni ya da yönü değiştirmez — bir okuma yüzeyinin kapısıdır. Emsal M15'in iş günü
  kesimidir: `tenants.day_cutoff` (`db/migrations/00064_add_the_business_day.sql:84-87`), owner'ın Account ekranında
  ayarladığı (`:89-92`), sütun düzeyi `GRANT UPDATE (day_cutoff) ON tenants` (`:92`), `audit_log`'da
  `tenant.day_cutoff_updated` (`internal/domain/tenant/daycutoff.go:79`), `Down`'da düşürülen yapılandırma sütunu
  (`00064:181-182`: *"configuration, not evidence (their history is audit_log's), and are dropped"*). Marka ayrı tablodadır
  (`tenant_branding`, `00028`: 256 KiB'lik `bytea` `tenants` okuyucularına taşınmasın diye); bu ayar küçük bir sayıdır.
- **Pencere bütün haftalardır** (denetim M3 — öneri seçildi): seçenekler **1 · 2 · 4 · 13 hafta**; *"N hafta"* = **bu
  hafta (pazartesiden bugüne) ve ondan önceki N−1 hafta**, işletmenin saat diliminde (`ledger.WeekOf`,
  `internal/domain/ledger/report.go:654`). Gerekçe: panelin raporu haftayı pazartesiden okur ve sınırdaki vardiyayı
  `StartedEarlier` dikişine koyar (`report.go:448-460`); gün sayılı bir pencere (*"son 7 gün"*) bir haftayı keser ve o
  hafta eksik toplamı `complete: true` ile verirdi. Bütün hafta penceresinde en eski görünen hafta tamdır ve panelle eşitlik
  testi her görünen haftada geçerlidir. *"1 hafta"* kullanıcının örneğidir (pazartesi günü yalnız o günü gösterir —
  adlandırılmış bedel).
- **Şema (00115) — Q48 (a), herkes kapalı** (2026-11-19): `ALTER TABLE tenants ADD COLUMN staff_hours_weeks smallint, ADD
  CONSTRAINT tenants_staff_hours_weeks_is_an_option CHECK (staff_hours_weeks IS NULL OR staff_hours_weeks IN (1, 2, 4, 13));` —
  varsayılansız, var olan ve yeni her satır NULL (= kapalı); owner açar.
  - Reddedildi: (b) yeniler açık / var olanlar kapalı ve (c) herkes açık (kullanıcı, 2026-11-19).
  - Ayrıca: `SET LOCAL lock_timeout = '1s'` (sıcak tablo, `deploy/README.md`), `GRANT UPDATE (staff_hours_weeks)
    ON tenants TO tappa_app` (sütun listesi, CLAUDE.md §6), `internal/db/privcatalog_test.go`'nun `privCatalogAllowed`
    satırı **genişler**: `column tenants tappa_app UPDATE name,business_type,timezone,day_cutoff` (`:414`) →
    `…,day_cutoff,staff_hours_weeks` (yeni satır eklenmez). **Kırmızıya dönecek var olan test:** `opAtVersionSkips`'in öncülü
    (`internal/db/operatorplaques_test.go:113-122` — operatör nesnesine dokunmayan, sıcak tabloya ACCESS EXCLUSIVE alan Down'lar)
    00115'in Down'ını (`tenants`'ta `DROP COLUMN`) kapsar → **00115** haritaya eklenir (M16'nın 108–110'u orada); bekçisi
    `TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject` (`internal/db/operatorapi_test.go:761`) yeşil kalır. İkinci kırmızı:
    `TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd` (`internal/db/m11schema_test.go:855`) — `m11Columns`'a
    `tenants.staff_hours_weeks` girdisi (roller ve harfler katalog dökümünden, `:466` satırının biçimiyle). **`Down`:** `DROP COLUMN staff_hours_weeks` (00064 emsali — yapılandırmadır, geçmişi `audit_log`'dadır;
    yapılandırma sütununun Down'ı reddetmez).
- **Panel:** Account ekranında, iş günü kesiminin yanında: *"Show My hours to staff"* aç/kapa + pencere seçimi (*This
  week · 2 weeks · 4 weeks · 13 weeks*). Yalnız **owner** yazar (marka emsali `brandEditorRole = "owner"`,
  `internal/domain/tenant/brand.go:125`); CSRF panelin mevcut kapısıyla. Değişiklik ve `audit_log` satırı **tek
  transaction'da**: action `tenant.staff_hours_updated`, `detail` `{"before":<n|null>,"after":<n|null>}`. Ekran metni
  (anlamı): *"A convenience for your staff. Their right to a copy of their own records does not depend on this setting."*
  (GDPR Art. 15). Kontrolün görünümü §9 sorusudur (panel ekranı — tap ailesi değil, ama yeni metin ve kontrol).
- **Okuyucular:** M16-31 ve M16-32 bu sütunu her istekte sunucuda okur (önbellek yok); `/app/v1/me` onu taşır (M16-31).
- **Canlıya çıkışta** ADR 0048'in `deploy/db/scoped-access.sql`'i (`origin/ops/tappa-scoped-access` dalı, `main`'de yok)
  yeni sütun yetkisi için yeniden koşulur — **kullanıcı** (state.md'nin 2026-11-18 Faz A notunun emsali).
- **Testler.** [G30-a] owner değiştirir → sütun + tam bir audit satırı (aynı transaction; audit yazılamazsa sütun da
  değişmez). [G30-b] manager, devre dışı owner ve başka işletmenin owner'ı → reddedilir, sütun ve audit değişmez.
  [G30-c] kümenin dışındaki değer (0, 3, 52) → CHECK reddi (23514). [G30-d] RLS: A bağlamı B'nin sütununu yazamaz (açık
  süzgeçsiz ifadeyle). [G30-e] `privcatalog` matrisi yeni sütun yetkisini görür. Komut: `GOTOOLCHAIN=go1.26.11 go test
  -run '<G30 testlerinin adları>' ./internal/domain/tenant/ ./internal/handler/ ./internal/db/`.

## M16-31 — Saatlerim: kişinin kendi kayıtlarının okuması + `POST /app/v1/hours`

- **Bağımlılık:** M16-30 · M16-19 (`qr_live` kanal değeri — Faz D, Faz G'den önce) · M9-05'i M16-30, M16-32, M16-33 ile
  birlikte **karşılar**
- **Kırmızı çizgi:** §4.3 (salt okuma), §4.5 (açık `tenant_id` **ve** `employee_id` + RLS), §5 *"Unutulan çıkış"*, §6, §7
- **Araç:** yapıcı + agent `tappa-db-migrator` (iki sqlc sorgusu; migration yok) · **Mercek: iki**
- **Commit:** `feat(hours): let an employee read their own hours`

**Amaç.** M9-05'in sunucu yarısı: çalışanın **yalnız kendi** kayıtları ve haftalık toplamı, panelin rapor aritmetiğiyle,
işletmenin ayarı ve penceresi içinde.

**Kabul kriterleri.**
- **İki sorgu (`db/queries/transactions.sql`'e):**
  - **Toplamlar:** `ListWorkedShiftEventsForEmployee` (öneri ad) — `ListWorkedShiftEvents`'in (`transactions.sql:970`)
    sütun listesi, yüklemleri ve **`LIMIT sqlc.arg('row_limit')`**'i birebir, ek yüklem `t.employee_id = @employee_id`;
    satırlar `ListWorkedShiftEventsRow`'a çevrilir (`timesheet.go:6-11`'in deseni) ve `events()` (`report.go:729`) +
    `accumulate()` (`report.go:1012`) ile toplanır — **ikinci aritmetik yok**. **Pencere ve sınır `Reader.Hours`'unkidir:**
    okuma `[hafta.From, hafta.To + closingTail)` (`report.go:520-522`; `closingTail = tap.StaleOpenIn`, `:165`) ve
    `row_limit = readLimit(cap)`. **Sınır bu okumanın parametresidir** (denetim N1): `truncatedBy` (`report.go:644`) sabit
    `ReportEventCap`'e (`:127`, 20000) karşılaştırır ve küçültülmüş bir `row_limit` onu hiç tetiklemezdi; yeni okuma
    `cap` alır (üretimde `ReportEventCap`) ve kesilmeyi `rowsRead > cap` ile kendisi söyler. Kesilmişse
    `totals.truncated: true` ve `complete: false`. Sorgu bilerek
    `type IS NOT NULL AND NOT practice` taşır (aynı bölüm): reject/ignored ve practice satırları saate girmez.
  - **Liste:** `ListOwnRecords` (öneri ad) — kişinin haftadaki **her** satırı, `reject`/`ignored` dahil (onların `type`'ı
    NULL olabilir: `00005_create_transactions_audit_reviews.sql:74-75`). Sütunlar: `id`, `occurred_at`, `type`, `channel`,
    `verdict`, inceleme sonucu (`transaction_reviews` LEFT JOIN), `location_id` + `site_*` (holding mekân adı), `practice`, ve
    satırın çapası olduğu talebin durumu (`punch_claims` + `punch_claim_decisions` LEFT JOIN; karar satırı yok = bekliyor,
    `outcome` `approved`/`rejected` — `00068:227-237`; kararın `note`'u **seçilmez**). **Seçilmeyen:** `gps_lat`, `gps_lng`,
    `source_ip` (ledger docket'inin kuralı, `internal/domain/ledger/ledger.go:177-178`: *"no Latitude, no Longitude, no
    SourceIP, and no PolicyContext"*), ayrıca bu kartın kararıyla `trust_score` ve kayıt notu (çalışan ekranları 2026-11-02
    kuralı — güven puanı ve kural notu yok). Yüklemler: `tenant_id = @tenant_id AND employee_id = @employee_id AND
    occurred_at >= @from AND occurred_at < @to` (çıplak parametreler, CLAUDE.md §6 API-8) `ORDER BY occurred_at, id`
    `LIMIT @limit` + keyset `(occurred_at, id) > (@after_at, @after_id)`. Plan `transactions_tenant_employee_occurred_idx`
    (`00005:175`) — `EXPLAIN` [G31-f]'de.
- **Kapsam kimlik bilgisinden:** `tenant_id`, `employee_id` doğrulanmış cihaz satırından (M16-05); gövde kişi taşımaz;
  okuma `WithTenant` içinde.
- **Sıra:** imza doğrulaması → cihaz kovası (`AllowDevice`) → `tenants.staff_hours_weeks` okuması (aynı transaction) →
  sorgular. **Ayar NULL ise `404`, gövde `{"error":"not_found"}`** (kapalı sözlüğün var olan kodu, `internal/handler/app.go`
  `appErrors`). Gerekçe: imza önce doğrulandığından imzasız biri ayarı yoklayamaz (401 hep aynı); kapalıyken çalışanın
  görebileceği bir kaynak **yoktur** ve `not_found` ucu tanımayan eski sunucunun cevabıyla aynıdır. Yeni kod eklenmez.
- **Pencere sorguda:** pencere başı `P` = bu haftanın pazartesisi − (N−1) hafta, sunucu saatiyle ve işletmenin diliminde
  (`WeekOf`). `@from = max(istenen haftanın pazartesisi, P)` (bütün hafta penceresinde her zaman istenen pazartesi). İstenen
  hafta `P`'den önce ya da bu haftadan sonraysa **400 `bad_request`** (CLAUDE.md §7 aralık sınaması).
- **`hasPrev` / `hasNext`:** `hasPrev` ⇔ istenen pazartesi − 7 gün ≥ `P` (önceki hafta pencerenin içinde); `hasNext` ⇔
  istenen pazartesi + 7 gün ≤ bu haftanın pazartesisi. Satır olup olmadığına bakılmaz (boş hafta da gezilir).
- **Deaktive kişi** (cihazı bilerek canlı — 2026-08-07): **okur** (kendi verisidir, kayıt yazılmaz). Bu sunucu
  davranışıdır ve bu kartta sabitlenir; M16-33'ün §9 sorusu yalnız ekran metnini sorar.
- **Tel sözleşmesi** (M16-07'nin tablosuna satır; `TestAppContract_RequestShapes` / `TestAppContract_ResponseShapes`
  genişler). Kimlik: imzalı okuma (`App-Device-Id`, `App-Timestamp`, `App-Signature`; kanonik önek `tappa-app-read-v1`).
  `GET` değil: imzalı okuma sorgu dizgesini reddeder (`internal/appdevice/read.go:114`).
  - İstek: `{"week"?: "YYYY-MM-DD", "after"?: "<cursor>"}` — `week` bir **pazartesi** olmalı (değilse 400), yoksa bu
    hafta; `after` önceki yanıtın `nextCursor`'u. Çözümleme katı (`DisallowUnknownFields`).
  - Yanıt (200):
    `{"week":"YYYY-MM-DD","weeksVisible":1|2|4|13,"hasPrev":bool,"hasNext":bool,`
    `"totals":{"workedMinutes":int,"awaitingMinutes":int,"openEntries":int,"checkInMissing":int,"truncated":bool,"complete":bool},`
    `"days":[{"date":"YYYY-MM-DD","workedMinutes":int}],` (tam 7 öğe, pazartesiden)
    `"records":[{"date":"YYYY-MM-DD","at":"HH:MM","venue":string,"direction":"in"|"out"|"","channel":"nfc"|"qr"|"qr_live"|"manual"|"other",`
    `"state":"approved"|"waiting"|"rejected"|"repeat","claim":"pending"|"approved"|"rejected"|null}],`
    `"nextCursor":string|null}`
  - Adlar `/v1/timesheets`'in modelidir (`internal/handler/api/timesheets.go:34-38`). **Dakikalar tam ve tabanlıdır** (§6;
    `timesheets.go:43-45`). `date`/`at` işletmenin yerel takvimi ve duvar saati, sunucuda.
  - **`channel`** kapalı kümedir ve Faz D'nin `qr_live` değerini (00112, M16-19) içerir; Faz G Faz D'den sonra koşar.
    Kümede olmayan bir kanal değeri (ileride eklenen) 500 vermez ve satır atlanmaz: alan `"other"` olur ve sunucu bir hata
    satırı loglar (öneri; uygulamalar *"Other"* çizer). [G31-k].
  - **`state` eşlemesi:** `ok` → `approved`; `flag` + onay → `approved`; `flag` kararsız → `waiting`; `flag` + ret ve
    `reject` → `rejected`; `ignored` → `repeat`. `type` NULL satırın `direction`'ı `""`'dir. `channel='manual'` satırı
    müdürün adını taşımaz. Tarihsel `practice=true` satırı listede **yoktur** (ADR 0008). `venue`: ledger'ın kendi
    adlandırması (holding dokunuşunda eş mekânın adı — `atSite`, `ledger.go:853`).
  - **Sayfa ve toplam:** `records` hafta başına en çok **200** öğe + `nextCursor` (`own_<occurred_at unix nano>_<id>_<bağ>`;
    bağ = cihazın kişisi + hafta özeti — başka kişinin ya da haftanın imleci 400; `/v1/timesheets`'in `tms_` imleç kuralı).
    `totals` ve `days` **sayfadan bağımsızdır**: her sayfada haftanın tamamı için aynı değer.
  - **`complete`** = `awaitingMinutes == 0 && openEntries == 0 && checkInMissing == 0 && !truncated` (`timesheets.go:36-37`'nin
    kuralı). Bütün hafta penceresinde pencere hiçbir haftayı kesmez.
- **`GET /app/v1/me` eki:** bugün `{"employeeName","brand","direction"}` (`internal/handler/apptap.go:76-81`). Eklenir:
  `"hoursWeeks": 0|1|2|4|13` (`0` = kapalı). [G31-h] sözleşme şekli.
- **Testler** (DB, gerçek Postgres). [G31-a] Eşitlik: tam bir hafta için `totals.workedMinutes` = aynı hafta ve kişi için
  `Reader.Hours`'un `PersonHours.Worked`'ünün dakikaya tabanı (birebir); yalnız tam haftalarda. [G31-b] RLS izolasyonu — açık
  süzgeçsiz sorguyla A bağlamı B'nin satırını görmez. [G31-c] Aynı tenant'ta X'in cihazı Y'nin satırını almaz; `employee_id`
  yüklemi düşürülünce kırmızı. [G31-d] Ayar NULL → imzalı istek 404 + `{"error":"not_found"}`; imzasız istek her ayarda 401.
  [G31-e] Pencere 1 hafta iken geçen haftanın satırı yanıtta ve sorgu sonucunda yok; geçen haftayı istemek 400; `hasPrev`
  false. [G31-f] `EXPLAIN` indeksi kullanır. [G31-g] Deaktive kişinin cihazı okur (200). [G31-i] `reject`/`ignored`
  listede `rejected`/`repeat`, toplamda yok; açık giriş → `complete:false`, `openEntries:1`. [G31-j] 201 kayıtlı haftada iki
  sayfa, ikisinde aynı `totals`; başka haftanın imleci 400. [G31-k] `qr_live` satırı `"qr_live"` döner. [G31-l] `cap = 3` ile 4
  satırlık hafta → `truncated:true`, `complete:false`; `cap = 4` → `truncated:false`; üretim kurulumu `cap = ReportEventCap`
  geçirir (kurucu testi).
- M9-05'i **M16-40 kapatır** (state.md; bu kart değil).

## M16-32 — Saatlerim: tarayıcı sayfası

- **Bağımlılık:** M16-31 · kullanıcının §9 kararı (görünüm, kaşeler)
- **Kırmızı çizgi:** §4.5, §9
- **Araç:** yapıcı + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(hours): show an employee their own hours in the browser`

**Amaç.** Tarayıcıyla dokunan çalışanın (ürünün varsayılan yolu) aynı Saatlerim'i görmesi (ADR 0045 §2.1).

**Kabul kriterleri.**
- **Önce §9 sorusu** (AskUserQuestion + mockup; tek soru, iki platformla ortak — M16-33 (3) aynı cevabı kullanır):
  **durumların görünümü** — (a) panelin kaşeleri (APPROVED / FLAGGED / REJECTED, docket motifi; FLAGGED altında *"Waiting
  for your manager"*); (b) düz kelimeler (*Counted · Waiting for your manager · Not counted · Repeat*), kaşesiz. ⚠️ 2026-11-02
  kararı çalışanın **sonuç ekranından** kaşeyi kaldırdı ve `flag` ekranı onaydan söz etmez (CLAUDE.md §9, §4.6 notu); Saatlerim
  ayrı ekrandır, soru bunu açıkça yazar. **Öneri: (b)** — çalışan ekranlarının 2026-11-02 sadeliğiyle uyumlu.
- **Sayfaya link yoktur** (denetim A2-5: *"Activation complete"* korumalı bir §9 ekranıdır ve davet e-postası ayrı bir şablon
  değişikliğidir — ikisi de Faz G dışı, *Sonra*). Sayfa yer imiyle ya da uygulamanın ana ekranından değil, adresiyle açılır;
  bu, çalışanın tarayıcı yolunda Saatlerim'i **bulmasını** zorlaştırır — adlandırılmış bedel.
- Rota `GET /me/hours?week=YYYY-MM-DD&after=<cursor>` (ana host; ikisi de dış girdi — M16-31'in aralık ve imleç kuralı, dışı
  400). **Sayfalama:** M16-31'in 200'lük sayfası; sayfanın altında *"Show more"* bağlantısı aynı `week` ve `nextCursor`'la
  bir sonraki sayfayı açar (betik yok, tam sayfa); toplamlar her sayfada aynı. Hafta gezinmesi `hasPrev`/`hasNext`'ten
  *"Previous week"* / *"Next week"* bağlantıları.
- Kimlik `tappa_session` (`internal/session/cookie.go:33`; `Path: "/"`, `:184`). Aynı domain okuması (M16-31), kişi çerezden.
- **Oturum yok** → düz sayfa (*"Open this on the phone you clock in with."*), 200, sorgu yok. **Oturum var, ayar kapalı** →
  genel 404 sayfası, kayıt yok.
- Salt okuma; form yok; `Cache-Control: no-store`. Logo/accent yok (sayılı marka slotları genişlemez — CLAUDE.md §9).
- **Testler.** [G32-a] Çerez X → yalnız X'in satırları. [G32-b] Ayar kapalı → 404, gövdede kayıt metni yok. [G32-c]
  Oturumsuz → düz sayfa, sorgu koşmaz. [G32-d] Panel golden'ları değişmez (`TestHoldinglessViews_AreByteIdenticalToTheGolden`).
  [G32-e] 201 kayıtlı haftada *"Show more"* ikinci sayfayı açar, toplam iki sayfada aynı.

## M16-33 — Uygulama ana ekranı ve Saatlerim — iOS + Android

- **Bağımlılık:** M16-31, M16-13, M16-17 · kullanıcının §9 kararı (bu kartın **ilk adımı**)
- **Kırmızı çizgi:** §9, §4.2 (bu ekranlarda konum çağrısı yok), §7
- **Araç:** iki yapıcı paralel (iOS, Android) + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(mobile): add the home and my-hours screens`

**Amaç.** Uygulama plaketsiz açıldığında gösterilecek ana ekran; ayar açıksa oradan Saatlerim, her zaman ayarlar (M16-38).

**Kabul kriterleri.**
- **Önce §9 onayı** — AskUserQuestion, iki platformun mockup'larıyla, seçenekleriyle. Bugün aktive uygulama açılışta doğrudan
  tap köküne gider (`mobile/ios/Taptime/RootView.swift:35-36`) ve `/app/v1/me` *"the app's home screen before any plaque is
  read"*dir (`internal/handler/apptap.go:83-84`); Q41 (a) iPhone 7–X'in tek ekranını tap ekranının kendisi yaptı.
  1. **Ana ekran:** (a) **tap ekranı ana ekrandır** — bugünkü ekran aynen, altında iki küçük, accent'siz metin bağlantısı
     (*"My hours"* — yalnız `hoursWeeks > 0` iken — ve *"Settings"*); (b) ayrı bir ana ekran — ad, işletme, iki büyük satır
     (*My hours*, *Settings*) ve bir *"Tap a plaque to clock in"* cümlesi; tap ekranı yalnız plaket/NFC/QR ile açılır.
     **Öneri: (a)** — tek ekran, tek düğme korunur; değişen yalnız düğmenin altındaki iki küçük bağlantıdır.
  2. **iPhone 7–X'in Core NFC düğmesi:** (a)'da değişmez (ana ekran = tap ekranı). (b) seçilirse Q41 (a)'nın tek ekranı
     bozulur: ana ekrana bir *"Clock in"* düğmesi gerekir (Core NFC'yi başlatır) — bu, M16-17'nin tap kökünü (`TapRootView`,
     launch yolu) değiştirir, `/app/v1/me`'nin açıklaması *"home screen"* olmaktan çıkar (`apptap.go:83-84` yorumu güncellenir)
     ve CLAUDE.md §9 *"Uygulama ekranları"* paragrafına *"plaketsiz açılış ana ekrandır; 7–X'te tap düğmesi oradadır"*
     cümlesi girer. Bu cevabın işi bu kartta yapılır.
  3. **Saatlerim'in durum görünümü:** M16-32'nin sorusuyla **aynı** cevap (orada sorulmadıysa burada sorulur).
  4. **Deaktive kişinin Saatlerim'i:** (a) aynen gösterilir (öneri — sunucu okumayı M16-31'de zaten verir); (b) başlığın
     üstünde *"Your account is deactivated."* satırıyla gösterilir.
  5. **Ayarlara giriş:** 1 (a)'da düğmenin altındaki *"Settings"* bağlantısı; 1 (b)'de ana ekranın satırı. (Ayrı soru değil —
     1'in cevabından çıkar.)
  Cevaplar kart düzeltmesine yazılır; cevapsız bir şey kurulmaz.
- Plaket linki / NFC / QR açılışı doğrudan tap ekranını açar; ana ekran araya girmez.
- **Ayar kapalıyken** (`hoursWeeks` = 0) Saatlerim'e hiçbir giriş yoktur; açık ekrandayken `not_found` gelirse ekran kapanır
  ve `/me` tazelenir. Pencere adlandırılır (*"This week"* / *"Last 2 weeks"* …); önceki hafta düğmesi `hasPrev` false iken
  yoktur. 200'den fazla kayıtlı haftada liste sonunda *"Show more"* `nextCursor`'la devam eder.
- Saatler cihazda **saklanmaz** (önbellek, dosya, `UserDefaults`/`SharedPreferences` yok).
- **Log kemeri.** R7M'nin tetikleyicileri (`scripts/redline-check.sh:2642`) bugün saat, mekân ya da ad kelimelerini taşımaz.
  Bu kart R7M'ye `workedMinutes|venue|employeeName|records` ekler (gürültü önce ölçülür: değişiklikten önce ve sonra
  `bash scripts/redline-check.sh` çıktısı karta yazılır) ve R7M'nin pinini genişletir: **`TestRedline_M16_MobileLogCalls`**
  (`cmd/tappa/scripttest/redline_m16_test.go:217`). Ayrıca [G33-a] Android JVM testi ve [G33-b] iOS birim testi: Saatlerim
  yanıtını işleyen kod yolunun log çağrısı yapmadığını sahte bir logger ile ölçer.
- İki platform tek metin listesi (Android `IosCopyTest` deseni). Konum API'si yok — M16-13/M16-17'nin çağrı-yeri izin
  listeleri değişmez.
- CLAUDE.md §9 *"Uygulama ekranları"* paragrafına ana ekran ve Saatlerim kararı (bu kartla).

## M16-34 — ADR 0049'un kabulü ve canlı taban ölçümü

- **Bağımlılık:** — (M16-30 … M16-33 ile paralel yürüyebilir)
- **Kırmızı çizgi:** §4.5, §4.6 (imzası çıkmış telefonun dokunuşunun audit satırı — kullanıcı onayladı, 2026-11-19), §5, §7
- **Araç:** **yapıcı alt ajan** belge değişikliklerini yazar (CLAUDE.md §10); ana oturum yalnız AskUserQuestion'ı sorar ve
  üretimde **salt okuma** sorgularını koşar (ya da kullanıcı koşar) · **Mercek: iki**
- **Commit:** `docs(adr): accept sign-in with a work email`

**Amaç.** [ADR 0049](../adr/0049-calisanin-e-postayla-girisi.md)'u kabul ettirmek, açık sorularını cevaplatmak ve uç canlıya
çıkmadan önce bugünün sayılarını ölçmek.

**Kabul kriterleri.**
- **Soru adımı yapıldı** (2026-11-19, kullanıcı, AskUserQuestion): Q44 (a) 24 saat · Q45 (b) yalnız uygulama · Q49 (a) owner
  ayarı, varsayılan kapalı · **T59 tersine çevirmesi (a) evet** — bugün iptal edilmiş oturumun dokunuşu yalnız WARN bırakır
  (ADR 0010:219-224; `internal/handler/tap.go:455-459`; T59 DÜŞÜK-1); artık listedeki ve girişi açık işletmelerde, yalnız
  tarayıcı yolunda bir `tap.signed_out_phone` audit satırı yazılır ve anomali listesi (2) görünür. Reddedildi: T59 (b) hayır
  (kullanıcı, 2026-11-19). **Bu kart yalnız kabul düzenlemelerini yapar.**
- ADR 0049 durum satırı *Kabul edildi (TARİH)* + *Kabul notu* (cevapları ve seçilen tarifleri adlandırır).
- ADR 0010'a not (*"listedeki ve e-postayla girişi açık işletmelerde artık bir `tap.signed_out_phone` audit satırı da
  yazılır — ADR 0049 §2.6"*) · ADR 0026'ya not (aktivasyonu başlatan davet artık çalışanın isteğiyle de basılabilir; tamamlanma — tarayıcı **ve**
  uygulama — aynen) · ADR 0022'ye not (yeni gönderim türü, alt bütçesi, kaynak kovası, log anahtarları, owner uyarısının
  `SendExempt`'i) · **ADR 0005'e risk eki:** *"posta kutusu + işverenin herhangi bir aktif plaketi = başka birinin kimliği"*,
  azaltmalar ve tespit (ADR 0049 §3); `TestADR0005_TheRiskCountMatchesTheTable` ve `TestADR0005_TheAnchorCountsMatchTheProse`
  yeşil.
- **Canlı taban ölçümü** (salt okuma; üretimde kullanıcı ya da ana oturum — yapıcı değil; sayılar kabul notuna): (1) KF ve
  KM'de `employees.email` dolu aktif çalışan sayısı; (2) iki işletmede aynı adresi taşıyan çalışan sayısı; (3) son 30 günde
  saat ve gün başına basılan davetin en yükseği, 50/sa · 300/gün ve süreç kesicisinin 300/sa'ine karşı; (4) son 30 günde
  aktivasyon başarısızlığı oranı (`activation.*` audit satırları); (5) K-22 durumu — bilgi (giriş K-17'yi kullanmaz); (6) son
  30 günde üretim log'undaki *"tap page: a signed-out phone tapped"* WARN satırlarının sayısı (SigNoz, salt okuma) — T59 (a)'nın
  audit hacmini öngörür.
- **CLAUDE.md §5 cümlesi bu kartta yazılmaz** — kodu M16-35 yazar.
- `GOTOOLCHAIN=go1.26.11 go test -run 'TestEveryNamedTestExists|TestADR0005_TheRiskCountMatchesTheTable|TestADR0005_TheAnchorCountsMatchTheProse' ./cmd/tappa/`
  yeşil.

## M16-35 — E-postayla giriş: sunucu

- **Bağımlılık:** M16-34 (ADR 0049 kabul) · kullanıcı: *"Taptime Demo"* açılmış (UUID'si config PR'ı için)
- **Kırmızı çizgi:** §4.5 (tenant bilinmeden adres çözümü), §4.6 (imzası çıkmış telefonun dokunuşu görünür olur), §4.7 + §7,
  §5 (aktivasyon), §6
- **Araç:** yapıcı + agent `tappa-db-migrator` (00116, 00117) · **Mercek: iki**
- **Commit:** `feat(signin): send an employee a sign-in link to their work email`

**Amaç.** ADR 0049'un tarifini kurmak: kayıtlı adresli aktif çalışan kendi isteğiyle yeni bir aktivasyon linki alır —
kehanetsiz, ayrı bütçeli, **varsayılan kapalı**; tamamlanma (tarayıcıda ya da uygulamada) yine plakette ve işverene görünür.

**Kabul kriterleri.**

*Açılış kapısı (denetim H3).*
- Env **`TAPPA_EMAIL_SIGNIN_TENANTS`** — virgülle ayrılmış tenant UUID'leri. **Boş = rota hiç bağlanmaz** (`/app/v1`
  yönlendiricisi uca kayıt yapmaz); `/app/v1/signin/request`'e istek, bilinmeyen bir `/app/v1` yolunun cevabını bayt bayt
  alır (`not_found` 404). Bozuk değer açılışı reddeder (`TAPPA_TAG_KEK_PREVIOUS` kuralı, `internal/config/config.go:425`).
  `deploy/k8s/05-config.yaml`'a **boş** değerle girer.
- Bu kartın birleşmesi (= deploy) **hiçbir işletmeyi açmaz**. Açılış ayrı bir **config PR**'ıdır: yalnız *"Taptime Demo"*
  UUID'si ve [G35-a]'nın beklediği değer; kullanıcı onaylar ve birleştirir. KF/KM'nin listeye girmesi Faz G dışı kullanıcı
  kararıdır.
- İkinci kapı işletmenin `tenants.email_signin` ayarıdır — Q49 (a), varsayılan kapalı (aşağıda *Panel*). Reddedildi: Q49 (b)
  yalnız env listesi (kullanıcı, 2026-11-19).

*Uç ve tek cevap.*
- `POST /app/v1/signin/request`, imzasız, gövde `{"email": string}` (katı; 254 baytı aşan ya da `@`'sız → yine tek cevap;
  yalnız JSON bozuksa 400). Dosya `internal/handler/appsignin.go` — R7A'nın `app*.go` kapsamına kendiliğinden girer
  (`scripts/redline-check.sh:2589-2593`); R7A tetikleyicilerine `e_?mail|address` eklenir (gürültü önce ölçülür), pin
  `TestRedline_M16_AppSurfaceLogCalls`.
- Her durumda **202**, boş gövde, `Cache-Control: no-store`, ve süre **`[250 ms, 300 ms]` bandında** — reset yolunun bandı
  (`internal/handler/adminresetoutbox.go:44-49`: medyanlar `[resetRequestFloor, resetRequestFloor+50ms]`; taban değerinin
  gerekçesi `adminresetlimits.go:316-326`). Tek istisna kaynak kovası: 429 + `Retry-After` (adresten bağımsız).

*Çözümleme (00116, 00117).*
- `resolve_employees_for_signin(p_email citext, p_tenants uuid[])` → `(tenant_id, employee_id)`; SECURITY DEFINER, sahibi
  `tappa_resolver`, `SET search_path = pg_catalog, pg_temp`, `citext` eşitliği `OPERATOR(public.=)` (`00011:295-313`).
  Yüklemler: `e.status = 'active' AND e.email IS NOT NULL AND e.tenant_id = ANY(p_tenants)` **ve işletme ön süzgeci**:
  `t.suspended_at IS NULL AND t.email_signin` — bant için: kimliksiz istek başına yalnız **eşleşen ve açık**
  işletmeler için bir basım transaction'ı açılır (denetim *timing*; aynı yüklemler basım transaction'ında yeniden sınanır,
  aşağıda). `ORDER BY e.tenant_id, e.id LIMIT 5`. `REVOKE ALL ON FUNCTION … FROM PUBLIC`, `EXECUTE` yalnız `tappa_app`.
- Yetkiler (00116) — **yalnız yeni sütunlar** (`00105`'in kalıbı, `00105:89` `GRANT SELECT (api_code)`): `GRANT SELECT (id,
  tenant_id, email, status) ON employees TO tappa_resolver` (bugün `employees`'te hiç yetkisi yok) ve `GRANT SELECT
  (suspended_at, email_signin) ON tenants TO tappa_resolver`. ⚠️ **`tenants.id` ne verilir ne geri alınır:** `tappa_resolver`
  onu `00051:155`'ten beri tutar (`GRANT SELECT (id, api_enabled_at)`) ve API anahtarı ile OAuth çözümleyicileri ona join
  eder (`00105:181`); sütun yetkisi tek bittir, `Down`'daki bir `REVOKE SELECT (id …)` `/v1`'in API anahtarı ve OAuth
  kimliğini **bütün işletmeler için** kırardı (Malta Calculator dahil). **`Down`** sırayla: `origin = 'self'` satırı varsa
  reddeder; yoksa `DROP FUNCTION`, `REVOKE SELECT (id, tenant_id, email, status) ON employees FROM tappa_resolver`, `REVOKE
  SELECT (suspended_at, email_signin) ON tenants FROM tappa_resolver`, `DROP COLUMN`'lar. [G35-o] 00116'nın `Down`'ından sonra
  `resolve_api_key_by_hash` ve OAuth token çözümleyicisi hâlâ satır döndürür. 00116 `employee_invites`'e ve
  `tenants`'a `ALTER` uyguladığı için **`SET LOCAL lock_timeout = '1s'`** ile başlar (`deploy/README.md`; `00064` emsali).
- **`privCatalogAllowed`** (`internal/db/privcatalog_test.go:274`): **yeni** satırlar — fonksiyonun `EXECUTE`'u (`tappa_app`),
  `column employees tappa_resolver SELECT id,tenant_id,email,status` (sütun sırası `attnum`'dur); **genişleyen** satırlar —
  `column tenants tappa_app UPDATE name,business_type,timezone,day_cutoff` (`:414`) M16-30'dan sonra
  `…,day_cutoff,staff_hours_weeks,email_signin` olur; `column tenants tappa_resolver SELECT id,api_enabled_at,api_code`
  (`:420`) → `…,api_code,suspended_at,email_signin` (yeni satır **eklenmez**). Liste katalogdan dökülür (`TAPPA_PRIVCATALOG_DUMP=1`) ve satır satır okunur.
- İndeks (00117) **`00091`'in birebir kalıbı**: `-- +goose NO TRANSACTION`; **`lock_timeout` yok** (`00091:33-35`'in
  gerekçesi: NO TRANSACTION dosyada oturum ayarı ifadeden uzun yaşar); Up önce `DROP INDEX CONCURRENTLY IF EXISTS
  public.employees_email_idx`, sonra `CREATE INDEX CONCURRENTLY employees_email_idx ON public.employees (email) WHERE email
  IS NOT NULL` (`00091:37-47`); Down `DROP INDEX CONCURRENTLY IF EXISTS`; ve sonraki migration'ların Down'larını yürüyen
  testler bu dosyayı **atlar** — **iki** atlama haritası: `opAtVersionSkips` (`internal/db/operatorplaques_test.go:122`;
  bekçisi `operatorapi_test.go:761`) ve `aiLaterSkips` (`internal/db/adminidentity_test.go:946`; 00070'in üstündeki her NO
  TRANSACTION dosyasının orada olmasını `TestAILaterSkips_AreConcurrentIndexesOffTheIdentityTables`, `:956`, ister).
  Gerekçe: tek adres indeksi `(tenant_id, email)`'dir (`00003:83-84`) ve tenant bilinmeden kullanılamaz.

*Basım: tek tasarım — `EmailChannel`'ın kendisi (denetim Y1).*
- Bugün `IssueAndDeliver` adres kapılarını yalnız `*EmailChannel` için koşar (`internal/invite/manager.go:287`: `mailCh,
  toOwnAddress := ch.(*EmailChannel)` → `checkLimits` `:298`, `readRecipient` `:335`, `RecipientCapped` `:342`; `email.go:213-215`
  *"for it alone"*). **Seçim:** yeni bir kanal tipi **yazılmaz**; giriş daveti bir `*EmailChannel`'dan geçer. Üç adım (limitler
  kiracı kilidinin altında, dört adres reddi, posta kutusu tavanı) ve `Delivery.Recipient`'ın dolması **aynen** çalışır.
- **Kurucu:** `invite.NewSigninChannel(sink MailSink, rec auditRecorder) (*EmailChannel, error)` — `channel = "signin"`
  (alan dışa kapalıdır; `NewEmailChannel`, `email.go:244`, kanal almaz ve bugün yalnız K-17 kuyruğu alanı elle kurar,
  `queue.go:399`). Aktör yoktur (`actorID` nil — istek kimliksizdir).
- **Sink:** `signinOutboxSink`, **`internal/handler`** paketinde (bugünkü `MailSink` uygulaması `emailLinkSink`'in yanında,
  `internal/handler/inviteemail.go`). `RecipientCapped` aynı `sender.Capped`'e gider (`inviteemail.go:303-305` — panelle tek
  tavan örneği). `SendInvitation` teslimi **bloklamadan** FIFO'ya sunar ve kabul ederse yeni sentinel **`invite.ErrQueued`**
  döndürür (`*mail.SendError` değil); FIFO doluysa ya da kapanıyorsa `*mail.SendError` (sınıf `queue_full`).
- **Audit sırası:** `EmailChannel.DeliverInvite` (`email.go:309-365`) bugün `SendInvitation` döner dönmez satır yazar; bu kart
  ona tek dal ekler — `errors.Is(err, ErrQueued)` ise **satır yazmaz** ve `nil` döner. İşçi gönderir ve gönderimden **sonra**
  tam bir satır yazar (`invite.code_emailed`, `detail.channel = "signin"`, `message_id`) ya da `invite.undelivered` — **aynı
  `detail` biçimiyle**: bugün `emailedDetail`, `undeliveredDetail` ve kanalın `audit`'i dışa kapalıdır (`email.go:309-365`);
  işçi için dışa açık **`invite.DeliveryRecorder`** (`invite.NewDeliveryRecorder(rec, actorID, channel)` — panel davetinin `actor_id`'si korunur, `email.go:345-350` — `ActorID: c.actorID` `:347`; yöntemleri `Emailed(ctx,
  Delivery, messageID)` ve `Undelivered(ctx, Delivery, *mail.SendError)`) aynı yapıları yazar, `DeliverInvite` de onu kullanır
  (tek biçim); dolu FIFO'da
  `DeliverInvite` bugünkü başarısızlık dalından senkron `invite.undelivered` yazar. Her davet **tam bir** satırla biter —
  **süreç ölmedikçe**: işçi kuyruğundaki davet süreç ölümünde satırsız kalır (adlandırılmış bedel, ADR 0022 sayılı sınır 1'in
  eşi). FIFO 32 (`resetOutboxSize`, `adminresetoutbox.go:252`), tek işçi, 15 sn sınır, işçide panik o işi `undelivered`'a
  çevirir; `cmd/tappa`'nın boşaltma dizisine reset çıkış kutusunun yanında girer. [G35-p] panel yolu (gerçek `EmailChannel`,
  kanal `email`) bayt bayt değişmez — senkron tek satır; giriş yolunda kabul edilen gönderim **senkron satır yazmaz**, satırı
  işçi yazar.
- **`IssueParams`'a üç alan** — `Origin` (`"employer"` | `"self"`), `Gate func(ctx context.Context, q *store.Queries) error`,
  `ExpectAddress string`. **`Gate`, `ExpectAddress` ya da `Origin = "self"`, `*EmailChannel` olmayan bir kanalla gelirse
  `IssueAndDeliver` reddeder** (`OnlyWithoutAddress`'in `:288` kalıbı) — bir yapılandırma hatası panelin kilitsiz kanalından
  geçemesin.
- **Kapılar basım transaction'ının içinde**, `LockTenantForInviteLimits`'ten (`email.go:394-403`, `checkLimits`'in ilk
  ifadesi) **sonra**, `checkLimits`'in hemen ardından `Gate`'te, bu sırayla — önce veritabanı, en son süreç bütçesi: (1)
  işletme askıda değil; (2) `email_signin` açık; (3) **işverenin canlı daveti yok** (kilit altında — müdürün kapı
  ile basım arasında basması yarışı kapanır); (4) **bekleme süresi**: son 7 günde tüketilmiş `origin='self'` davet yok; (5)
  işletme ve kişi giriş bütçeleri (DB); (6) **süreç alt bütçesi** — `httpx.Limiter.Reserve` (`internal/httpx/ratelimit.go:207`)
  ile **ayrılır**, transaction commit edince `Keep` (`:224`), geri alınırsa `Refund` (`:240`): basımdan sonra düşen her şey
  (yönetici adresi, ASCII olmayan adres, `RecipientCapped`, adres değişti) alt bütçeyi **iade eder** — tek bir kötü adresin
  bütün işletmelerin 100/sa'ini yemesi kapanır.
- **Yönetici adresi basımdan önce elenir:** çözümleyici, adresi aynı işletmedeki bir `admin_users` adresine (`citext`,
  `OPERATOR(public.=)`) eşit olan çalışanı döndürmez — `tappa_resolver` `admin_users`'ın `tenant_id, email`'ini zaten okur
  (`internal/db/privcatalog_test.go:311`). `readRecipient`'ın aynı reddi (`ErrAddressIsAdministrators`) arkada kemer olarak kalır.
- **Adres yeniden okunur:** `readRecipient`'tan hemen sonra (`manager.go:335`) `to.Address` `ExpectAddress`'e `citext` olarak
  eşit değilse — owner o arada adresi düzeltmiş (`internal/domain/tenant/staffemail.go:172`) — yeni sentinel
  **`invite.ErrAddressChanged`** ile transaction geri alınır.
- **Giriş kanalı ret satırı yazmaz:** bugün bir adres reddi `RefusalReason` → `mailCh.refused` ile tenant'ın `audit_log`'una
  `invite.email_refused` yazar (`manager.go:351-358`, `email.go:371-389`). `channel = "signin"` iken `refused` **satır yazmaz**
  (yeni dal): retlerin çoğu kimliksiz isteğin ürünüdür ve değişmez tabloya sınırsız satır olurdu. `ErrAddressChanged` da satır
  yazmaz. Sonuç: giriş yolunun audit izi yalnız başarılı teslim (`code_emailed`) ve teslim hatası (`undelivered`) satırlarıdır.
- `IssueParams.Origin = "self"`, `TTL = 24 * time.Hour` (Q44 (a); reddedildi: 12 sa ve 1 sa — kullanıcı, 2026-11-19), `ExpectAddress` = çözümleyicinin eşlediği adres.
- **`employee_invites.origin` (00116):** `ADD COLUMN origin text NOT NULL DEFAULT 'employer', ADD CONSTRAINT
  employee_invites_origin_is_known CHECK (origin IN ('employer','self'))`; tablo düzeyi `INSERT` yetkisi (`00009:224`) yeni
  sütunu kapsar; `UPDATE` yetkisi yok. Aynı dosyada `tenants.email_signin boolean NOT NULL DEFAULT false` +
  `GRANT UPDATE (email_signin) ON tenants TO tappa_app`.
- **Yalnız e-posta modunda (denetim D-8):** rota yalnız `TAPPA_INVITE_DELIVERY=email` **ve** liste dolu iken bağlanır — panel
  modunun `ManagerVisibleChannel` basımı (`internal/handler/employeeactions.go:439-448`) kiracı kilidi almaz ve kapılar ona
  uygulanamaz. [G35-q] panel modunda, liste dolu olsa bile rota bağlı değil.

*Bütçeler (ADR 0049 §2.4) — nerede sayılır.*
- Kaynak adres 5 / 10 dk ve 20 / gün — **bellekte** (`httpx.Limiter`), istekte, çözümlemeden önce. Süreç alt bütçesi 100 / saat —
  **bellekte**, kapı (6)'da ayrılır, commit'te tutulur, geri almada iade edilir. Pod sayısıyla çarpılır — adlandırılmış bedel.
- İşletme 10 / saat, 30 / gün; kişi 1 / saat, 3 / gün — **veritabanında**, `employee_invites` satırlarından (`origin='self'`,
  `created_at`), kiracı kilidinin altında — podlar arasında tutarlı.
- **Mekân NAT'ı (denetim D6):** aynı mekânın Wi-Fi'ındaki personel tek bir kaynak adresi paylaşır; 10 dakikada altıncı kişi
  429 alır. Kova **yükseltilmez**; bedel adlandırılır ve ekran *"Too many tries from this network — wait a few minutes or use
  mobile data."* der (M16-36).
- **Kesici kaderi kanalı (denetim D7):** reset yolunun sayılı sınırı 13'ü (`adminresetoutbox.go:218-221`) bu yolda da vardır ve
  **daralır**: giriş gönderimleri ayrı alt bütçeyi tüketir, eşleşmeyen adres hiçbir bütçe harcamaz. Kalan kanal ADR 0049
  **§2.4**'te adlandırılır.
- **Bekleme süresinin yarışı (denetim D-3):** `consume` kiracı kilidini almaz; bir `self` tüketimiyle eşzamanlı ikinci bir
  `self` basımı bekleme kapısını geçebilir. **Seçim: sınırlamak, kilit almak değil.** Kilit sırası sorunu yoktur —
  `pg_advisory_xact_lock('invite-limits:' || tenant)` `consume`'un ilk ifadesi olsaydı sıra basımınkiyle aynı olurdu; ama o
  kilit işletmenin **her** aktivasyonunu (müdür davetleri dahil) davet basımının arkasına dizerdi. Sınır: kişi bütçesi (1/sa) yarışta en
  çok **bir** fazla davet bırakır; o davet de ayrı bir plaket dokunuşu ister, tamamlanırsa owner'a ikinci bir uyarı gider ve
  bir sonraki basım onu emekliye ayırır. Adlandırılmış bedel (ADR 0049 §4).

*İşverene görünürlük (denetim H4 (1), Y2, O1; üçüncü tur O-3, D-4, O-2, D-5, N2, N8, N9).*
- **Tüketim noktası `consume`'dur** (`manager.go:846`) ve `consume` **`Activation.Origin`**'i döndürür. Faz G, Faz D'den sonra
  koşar; M16-35 yazılırken tüketen çağıranlar: tarayıcı `Manager.Activate` (`:717`, handler `finishActivation`), uygulama
  `ActivateDevice` (`:791`, handler `App.ActivateComplete`, `internal/handler/appactivate.go:252`) **ve M16-23'ün iki NFC'siz
  yolu** (canlı QR ve müdür onayı — ikisi de daveti tüketir). Her tüketen handler, tüketim **döner dönmez** (`err == nil`,
  davet commit edilmiş) ve `Origin == "self"` ise tek `selfActivationNotice`'ı çağırır — oturum/cihaz verme adımından **önce**:
  `finishActivation` tüketimden sonra erken dönebilir (`session_issue_failed`, `internal/handler/activate.go:1086-1163`), uyarı
  yine gider. `selfActivationNotice` **hiçbir şey göndermez, yalnız bir iş başlatır** (aşağıda) ve hemen döner.
  **[G35-r] statik test:** taranan küme **hesaplanır**: `internal/invite`'ın AST'sinde gövdesi `consume`'u çağıran dışa açık
  `*Manager` yöntemleri; sonra `internal/handler`'da bu yöntemlerden birini çağıran her işlevin aynı gövdede
  `selfActivationNotice`'ı da çağırdığı ölçülür — eşleşme **tip bilgisiyle** (`go/types`: alıcı `*invite.Manager`), ad eşleşmesiyle
  değil; aksi hâlde `pages.Activate(v, look)` (`internal/handler/activate.go:1731`, alıcısı bir paket adı) yanlış eşleşirdi — M16-23'ün yeni bir yöntemi listeye kendiliğinden girer.
- **Owner e-postası — istek yolunda değil, sınırlı bir arka plan işi** (dördüncü tur B1, ana oturum kararı). Gerekçe: owner
  listesi 200'e kadardır (`resolve_admin_owners_by_tenant`, `00081`, `LIMIT 200`) ve her owner'a bir ileti gider; istek içinde
  tek gönderim bile `EmailSendGrace` (10 sn, `internal/invite/email.go:210`) sürebilir, uygulamaların okuma süresi ise Android
  15 sn (`mobile/android/app/src/main/kotlin/mt/taptime/app/wire/Transport.kt:27`), iOS 20 sn (`mobile/ios/Taptime/AppClient.swift:224`)
  — cevabı kaybeden uygulama `deviceId`'yi hiç öğrenmez, davet harcanmış, öteki cihazlar iptal, bekleme süresi kendi kendine
  kurtarmayı 7 gün kapatır. **Tarif:** `selfActivationNotice` işi `ownerNotices`'a (yeni, `internal/handler`) sunar — kendi
  sınırlı FIFO'su (32 iş; giriş çıkış kutusundan **ayrı** — kimliksiz istek onu dolduramaz, her iş bir **tüketimdir** —
  plaket dokunuşu, canlı QR ya da müdür onayı; sınırı `self` basım bütçesidir), **2 işçi**, iş başına **4 eşzamanlı gönderim**, gönderim başına `EmailSendGrace`, iş başına **toplam 120 sn**
  son süre; bağlam `context.WithoutCancel(ctx)` (parola bildiriminin emsali, `internal/handler/passwordnotice.go:311`).
  Kapanış boşaltmasına reset çıkış kutusunun `Drain` yönteminin (`internal/handler/adminresetoutbox.go:570`) yanında, `cmd/tappa`'nın
  `drainers` kümesinden girer (küme `cmd/tappa/main.go:1096`'da kurulur; `Drain` yöntemi `cmd/tappa/api.go:106-110`; bütçe kuralı
  `main.go:92-95`). **`ownerNotices.Drain` bağlamı dolunca döner** (son tur, madde 3): `drainers.Drain` her üyeyi bekler
  (`api.go:113-115`) ve bloklayan bir üye, ertelenen encode `Close`'unu (`sun.Zero` anahtar silmesi, ADR 0017) SIGKILL'in
  arkasına iterdi (`main.go:85-90`). Desen reset çıkış kutusunun **iki evresidir** (`adminresetoutbox.go:580-599`): tek paylaşılan
  bağlam (`main.go:1145-1155`), `ResetDrainGrace` 3 sn (`:269`), son `ResetDrainWriteReserve` 1 sn (`:278`) yalnız satır yazmaya
  ayrılır — gönderimler rezervin başında kesilir. **Açıkça: 120 sn'lik bir iş 3 sn'lik boşaltmaya sığmaz;** sonuç, zaman
  kalırsa `undelivered` satırlarıdır, kalmazsa **hiçbir şey** (pod ölümünün aynı boşluğu). **Adlandırılan dallar:** (i) boşaltmada
  **başlamamış her iş** için bir `undelivered` satırı — dolu FIFO dalının eşi; (ii) **kuyruk kapalı** dalı — HTTP boşaltması
  20 sn, owner boşaltması 3 sn olduğundan bu dal gerçekten olur; kapalı bir kanala gönderim panik verdiğinden sunum **reddedilir**
  ve senkron bir `undelivered` satırı yazılır; (iii) **işçide panik** `containNotice`'ın kalıbıyla (`internal/handler/passwordnotice.go:409`):
  sabit bir cümle, değer loglanmaz, bir `undelivered` satırı; (iv) audit eylemleri `admin.password_notice.*`'in kalıbıyla
  (`passwordnotice.go:229-230`): her `ownerNotices` işi bir **`kind`** alanı taşır ve konuyu, gövdeyi ve audit eylemini o seçer —
  `self_activation` → `employee.self_activation_notice.sent` / `.undelivered`; `deletion_request` (M16-37, Q46 (b)) →
  `employee.deletion_notice.sent` / `.undelivered`; ikisinde de `target` çalışan, `actor` yok, `detail`
  owner'ın `admin_user_id`'si (adres yok); (v) **bildirim sırası** çözümleyicinin `ORDER BY a.id`'sidir (`00081`) — süre kesince
  sıranın sonundaki owner'lar `undelivered` alır. Son süre ya da boşaltma kesince gönderilmemiş her owner için bir
  `undelivered` audit satırı (zaman kalırsa); FIFO doluysa tek bir senkron `undelivered` satırı, gönderim yok. **Pod ölümü:** kuyruktaki ve
  uçuştaki uyarılar gider ve satırsız kalır — giriş çıkış kutusunun aynı kabul edilmiş boşluğu; **panel sinyali dayanıklıdır**
  ve etkilenmez. Gönderim `Breaker.SendExempt` ile (emsal `passwordnotice.go:46`, `emailchangenotice.go:19`) kalır. Owner kimlikleri
  `resolve_admin_owners_by_tenant` (`00081`), adresi `passwordnotice.go`'nun alıcı yolundan (**ölç:** M14 sonrası kaynak
  kimliktir, ADR 0031 K-14). **Tavan:** owner başına günde 20 — bellekte, `httpx.Limiter.TryCharge`
  (`passwordnotice.go:336-339`'un deseni, anahtar owner kimliği). Konu `A phone was set up by email sign-in`; gövde (anlamı):
  *"<Employee> set up a new phone with their work email on <date>. Their other phones were signed out. If you didn't expect
  this, deactivate the account in Staff and contact them."* — çalışanın adresi yok.
- **İmzası çıkmış telefonun dokunuşu — kullanıcı onayladı (T59 (a), 2026-11-19); yalnız listedeki ve açık işletmede.**
  Bugün bilerek iz yalnız WARN'dur (ADR 0010:219-224, T59 DÜŞÜK-1). `GET /t`'nin iptal edilmiş oturum dalında
  (`internal/handler/tap.go:440`) **bu sırayla**: (1) işletme env listesinde mi — bellekte, okuma yok; (2)
  `email_signin` açık mı; (3) **ilerletmeyen SUN ön kontrolü** (`:480-483`'ün aynı fonksiyonu — CMAC, sayaç ilerlemez). İlk
  ikisi önce gelir ki KF/KM'nin iptal edilmiş dokunuşu ne yeni bir okuma ne yeni bir log satırı üretsin (ön kontrol hatada
  `tag_uid`'li satırlar loglar, `tap.go:494`, `:498`) — gerçekten bayt bayt aynı. *"İşletme"* = plaketin tenant'ı **ve** oturumun
  tenant'ı, ikisi **eşit** olmalı (holding dokunuşunda satır yazılmaz). Üçü de geçerse WARN'un
  (`:455-459`) yanına bir `audit_log` satırı: action `tap.signed_out_phone`, `target` çalışan, `detail` `{"channel":…}` —
  plaket UID'si, GPS, çerez, özet yok; kişi başına 10 dakikada en çok bir satır (bellekte `httpx.Limiter`). Ön kontrol
  geçmezse ya da işletme listede değilse bugünkü davranış **bayt bayt** aynı (KF/KM'nin `audit_log`'una hiçbir şey yazılmaz).
  Ön kontrol bir tekrarı ayırt edemez — aynı telefondaki eski bir URL de geçer; satır *"kanıtlı dokunuş"* değil *"plaket
  kanıtı taşıyan bir sayfa açılışı"*dır (adlandırılmış). **Yazmayan iki yer:** `POST /api/checkin`'in iptal edilmiş oturum
  WARN'u (`internal/handler/checkin.go:148-153` — sayfa iptalden önce açılmış, basış sonra; aynı dokunuşun `GET /t`'si satırı
  zaten yazdı) ve `tap.go:842`'nin aktivasyon iniş yönlendirmesi. **Uygulama yolu değişmez:** iptal edilmiş cihazın kimliği
  imzadan önce bir iddiadır (M16-07) — **uygulamadan gelen kayıp dokunuşlar panelde görünmez** (ADR 0049 §3). Reddedildi:
  T59 (b) satır yok (kullanıcı, 2026-11-19).
- **Panel (dayanıklı):** M6-11 anomali ekranında (1) *"Phones set up by email sign-in (last 30 days)"* — `employee_invites`
  `origin='self' AND used_at >= now() - interval '30 days'`; (2) *"Taps from a signed-out phone after an email
  sign-in"* — **tek yüklem:** `audit_log.at ∈ [i.used_at, i.used_at + 14 days]` ve aynı `target`, (1)'in aynı 30 günlük
  `used_at` penceresindeki davetler için. N = 14: bir ele geçirmenin fark edilmesi için iki iş haftası. Çalışan kartında
  *"Set up by email sign-in on <date>"*. Yeni sqlc sorguları açık `tenant_id`'li. Görünüm skill `tappa-brand`; panel metni
  kullanıcıya gösterilir.

*Panel — işletmenin e-postayla giriş ayarı (Q49 (a)).*
- Account ekranında owner'ın *"Email sign-in for staff"* aç/kapa'sı — M16-30'un kontrol deseni (yalnız owner, CSRF,
  `audit_log` `tenant.email_signin_updated` `{"before":bool,"after":bool}` aynı transaction'da). **Görünümü bu kartın kendi
  §9 sorusudur** (M16-30'un sorusundan ayrı): (a) iş günü kesiminin ve Saatlerim'in yanında bir aç/kapa satırı;
  (b) ayrı bir *"Staff sign-in"* kartı, açıklamasıyla. Metin (anlamı) *"Let staff with an email on file set up a new phone themselves. You'll get an email each time."*
- **Canlıya çıkışta** ADR 0048'in `deploy/db/scoped-access.sql`'i (`origin/ops/tappa-scoped-access`) yeni sütun ve fonksiyon
  yetkileri için yeniden koşulur — **kullanıcı**.

*E-posta (çalışana), tamamlanma, metinler.*
- ADR 0022 §8 şablon kuralları; konu `Your Taptime sign-in link`; gövde (anlamı): *"Someone asked to sign in to Taptime with
  this address at <Business>. If it was you, open this link on your phone within <TTL>, then tap your workplace's plaque. If
  it wasn't you, ignore this email — nothing changes."* (`<TTL>` = *"24 hours"*.) Aynı adres
  birden çok işletmede aktifse (holding dahil): işletme başına bir davet ve bir e-posta.
- **Tamamlanma bugünkü yoldur ve tarayıcıda da olabilir:** link (aynı telefonda) tarayıcıyı açar → ADR 0026'nın sihirbazı →
  ilk gerçek NFC; ya da kişi linki uygulamaya yapıştırır → `bind` → `complete`. Q45 (b) yalnız **isteği** sınırlar, tamamlanmayı
  değil. `Lookup` süresi dolmuş kodu reddeder (`manager.go:493-494`).
- **Bekleme süresinin bedeli:** ele geçirmeden sonraki 7 gün boyunca **gerçek** çalışan da kendi kendine kurtaramaz —
  isteği 202 alır ama e-posta gelmez. Uygulamanın sabit ekranı bu yüzden *"No email after a few minutes? Ask your manager
  for an invite."* der (M16-36); müdürün daveti bekleme süresine takılmaz.
- **Log ve audit:** adres, özeti, kod, link hiçbir log satırında, hatada ve `detail`'de yok (CLAUDE.md §7; ADR 0022 §10).
- **CLAUDE.md §5** *"Aktivasyon dokunuşu"*na cümle (kodla aynı commit): davet müdürün basışıyla **ya da** listedeki bir
  işletmenin kayıtlı adresli aktif çalışanının e-postayla isteğiyle basılır; tamamlanma iki yolda aynen.

*Testler* (DB, gerçek Postgres; `-race`'li olanlar işaretli).
- [G35-a] `05-config.yaml`'da anahtar var ve boş (`cmd/tappa/applinks_test.go`'daki `TestPackaging_TheAppValuesAreTheHandedOverOnes`'ın
  yanına). [G35-b] env boş → rota bağlı değil, `/app/v1/signin/request`'in cevabı bilinmeyen bir `/app/v1` yolunun cevabıyla
  **bayt bayt** aynı (durum, gövde, başlıklar `Date` hariç); listede olmayan işletmenin adresi → 202, davet satırı 0.
- [G35-c] çözümleyici üç yazımda bulur; `OPERATOR(public.=)` düşürülünce kırmızı.
- [G35-d] **tek cevap ve kapılar:** bilinmeyen · deaktive · davetli · yönetici adresi (çözümleyici eler) · askıda · işletme ayarı kapalı
  · canlı işveren daveti · bekleme süresi · işletme bütçesi · kişi bütçesi · alt bütçe · adres yarış içinde değişti — on ikisinde
  202, boş gövde, davet satırı 0 **ve `audit_log` satır sayısı değişmez** (giriş kanalı ret satırı yazmaz — `invite.email_refused`
  0). Basımdan sonra düşen retlerde (ASCII olmayan adres, `RecipientCapped`, adres değişti) süreç alt bütçesi **iade edilmiş**
  olur (`Reserve` → `Refund`; sayaç ölçülür). Yalnız eşleşen aktif adreste 1 satır `origin='self'`. Çıkış kutusu doluyken
  eşleşen adres: 1 davet satırı + senkron `invite.undelivered`.
- [G35-e] **bant:** medyanlar `[250 ms, 300 ms]` içinde — k = 0, 1 ve 5 eşleşen işletme; çıkış kutusu dolu; kesici açık;
  **kapı içi retler** (bekleme süresi, canlı işveren daveti, DB bütçesi — transaction açılır, basım olmaz) ve **basım sonrası
  ret** (`RecipientCapped`, adres değişti — geri alma). k = 5'te bant tutmazsa kart ölçümü yazar ve basımları tek
  transaction'a toplar ya da tabanı gerekçesiyle yükseltir (kullanıcı kararı).
- [G35-f] iki işletmede aynı adres → iki tenant'ta birer davet, RLS ile birbirini görmez, iki e-posta (sahte sink).
- [G35-g] **yarış:** kapı okumasıyla basım arasında müdür davet basar → giriş basımı olmaz (kilit sırası, `-race`, iki
  goroutine); owner adresi `CreateInvite`'tan sonra değiştirir → `ErrAddressChanged`, geri alınır, satır yok. Bekleme süresi
  yarışı (D-3): eşzamanlı `self` tüketimi + `self` basımı en çok bir fazla davet bırakır (ölçülür ve sınır yazılır).
- [G35-h] e-postayla basılmış davetle N eşzamanlı `complete` → tam 1 başarı (`-race`).
- [G35-i] **bütün tüketen yollar:** aynı davet türü tarayıcıda (`Manager.Activate`), uygulamada (`ActivateDevice`) ve M16-23'ün
  iki NFC'siz yolunda tamamlanır → hepsinde owner e-postası `SendExempt`'ten (sahte kesici reddetmeye ayarlıyken de gider) ve
  anomali listesinde satır; `finishActivation`'ın `session_issue_failed` dalında da uyarı gider; `origin='employer'`
  tüketiminde e-posta **yok**. [G35-r] statik çağıran taraması (yukarıda).
- [G35-j] sızıntı: bütün yolların log ve audit çıktısında **adresin (çalışanın ve owner'ların), davet kodunun ve `ActivationURL`'nin** hiçbir yazımı yok —
  giriş FIFO'su URL'yi tutar; işçinin **panik yolu** (panik değeri URL'yi taşıyabilir) ve owner uyarı işi dahil
  (`internal/invite/leak_external_test.go` emsali).
- [G35-k] 24 saati geçmiş link `lookup`'ta reddedilir.
- [G35-l] ayarı kapalı işletmenin adresi → 202, 0 davet.
- [G35-m] **`origin` varsayılanı (denetim D5):** panelin davet ucu, `/v1` personel ucu ve K-17 kuyruk işçisi davet basar →
  üçünde de `origin='employer'`; owner e-postası yok.
- [G35-n] listedeki ve ayarı açık işletmede, imzası çıkmış tarayıcı oturumuyla **geçerli SUN**'lu
  `GET /t` → `transactions` sayısı aynı, tam bir `tap.signed_out_phone` satırı; ikinci açılış 10 dakika içinde satır yazmaz;
  SUN'suz/bozuk SUN'lu açılış satır yazmaz; **listede olmayan** işletmede satır yazmaz. Anomali listesi (2) satırı yalnız
  `at ∈ [used_at, used_at + 14 gün]` iken gösterir (14. günün bir dakika sonrası görünmez). Var olan
  `TestTapDB_ASignedOutPhoneGoesToActivationAndWritesNothing` (`internal/handler/employeesignout_db_test.go:200`) bugün yalnız
  çıkış audit eylemini sayar; bu kart onu **genişletir** — listede olmayan işletmede **bütün** `audit_log` satırlarını sayar
  (adı o durumda doğru kalır). 
- **Kırmızıya dönecek var olan altı test — bu kartta güncellenir** (dördüncü tur N1 + son denetim): (1) `TestApiKeysCatalog`
  (`internal/db/apikeys_test.go:183`, `id || api_enabled_at || api_code`'u sabitler) — `suspended_at, email_signin` eklenir;
  (2) `TestApiKeys00051_DownGivesBackAndUpTakesItAgain` (`:353-375`; 00105/52/51 Down'larından sonra 0 `tenants` sütunu bekler
  ve 00051'in Up bekçisi başka bir sütun yetkisinde hata verir, `00051:57-67`) — `:346-352`'nin 00105 için anlattığı kalıpla
  **önce 00116'nın Down'ı** koşar, Up'ı en son. Test `holdingLockedSavepoint` içinde koşar (`apikeys_test.go:358`) ve sarmalayıcı,
  bir savepoint `holdingDownTables` dışındaki bir tabloda güçlü kilit tutarsa düşer (`holdingmigration_test.go:105-140`,
  `holdingtap_test.go:2066-2077`, `:1737-1775`): 00116'nın Down'ı `employee_invites`'ta `DROP COLUMN origin` ile ACCESS EXCLUSIVE
  alır → **`employee_invites` `holdingDownTables`'a eklenir** ve savepoint içinde, Down'dan önce `origin='self'` satırları kenara
  konur. **Ölç:** `REVOKE SELECT (…) ON employees` güçlü kilit alıyor mu; alıyorsa `employees` de eklenir; (3) kimlik yürüyüşleri 70'in üstündeki her transaction'lı Down'ı koşar
  (`internal/db/adminidentity_test.go:865-888`) ve 00116'nın Down'ı `origin='self'` satırı varken reddeder — 00095/00108/00109'un
  kenara koyma adımının (`:872-887`) eşi eklenir; (4) `TestAILaterSkips_AreConcurrentIndexesOffTheIdentityTables` `transactions`
  dışındaki indeksi reddeder (`:997-998`, *"widen this"*) — 00117 `employees` üstündedir, test genişletilir (kimlik testlerinin
  `employees` kataloğunu sabitlemediği ölçülerek); (5) `opAtVersionSkips` (`internal/db/operatorplaques_test.go:113-122`'nin
  öncülü: operatör nesnesine dokunmayan, sıcak tabloya ACCESS EXCLUSIVE alan Down'lar) — **00116** eklenir (M16'nın 108–110'u
  orada). 00117 iki atlama haritasına da girer (yukarıda); (6) `TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd`
  (`internal/db/m11schema_test.go:855`, matris `:911-985`) — `m11Columns`'a `tenants.staff_hours_weeks` (00115, M16-30'un işi),
  `employee_invites.origin` ve `tenants.email_signin` (00116) girer; `:466`'nın `tenants.suspended_at` satırına `"tappa_resolver":
  "S"` eklenir; fonksiyon matrisine `resolve_employees_for_signin(citext,uuid[])` (`:627-680`'in kalıbı).
- [G35-o] 00116'nın `Down`'ından sonra `resolve_api_key_by_hash` ve OAuth token çözümleyicisi hâlâ satır döndürür (`tenants.id`
  yetkisi dokunulmadı).
- [G35-p] panel yolu değişmez; giriş yolunda kabul edilen gönderim senkron satır yazmaz (yukarıda).
- [G35-q] `TAPPA_INVITE_DELIVERY=panel` iken liste dolu olsa da rota bağlı değil.
- [G35-t] **cevap gönderimi beklemez:** iki owner'lı işletmede, her gönderimi 2 sn tutan sahte sink'le `self` tamamlanması →
  HTTP cevabı ilk gönderim bitmeden döner (cevap < 500 ms, iki gönderim sonra biter); boşaltma bütçesi dolunca kalan owner
  `undelivered` satırı alır; FIFO doluyken tek senkron satır. **Boşaltma:** takılı bir sink ve 200 owner'la `Drain(3 sn)`
  ≤ 3 sn + ε içinde döner; başlamamış işler ve kesilen owner'lar `undelivered` alır (zaman yettiyse); kuyruk kapandıktan sonra
  sunulan iş panik vermez, senkron `undelivered` yazar; işçide panik değer loglamadan `undelivered` yazar.
- [G35-s] `IssueAndDeliver`, `Gate`/`ExpectAddress`/`Origin="self"` ile `*EmailChannel` olmayan bir kanalı reddeder.

## M16-36 — E-postayla giriş: uygulama ekranları

- **Bağımlılık:** M16-35, M16-12, M16-16 · kullanıcının §9 kararı (aktivasyon ailesi)
- **Kırmızı çizgi:** §9 (aktivasyon ailesi), §7
- **Araç:** iki yapıcı paralel (iOS, Android) + skill `tappa-brand` · **Mercek: iki**
- **Commit:** `feat(mobile): sign in with a work email`

**Amaç.** Daveti olmayan ya da kaybetmiş çalışanın uygulamadan kendi adresine giriş linki istemesi.

**Kabul kriterleri.**
- **Önce §9 sorusu** (AskUserQuestion + mockup; aktivasyon ailesi): (1) kod ekranındaki giriş — (a) *"No link? Get one by
  email"* metin bağlantısı (öneri), (b) ikinci bir düğme; (2) sabit cevap ekranının metni (aşağıdaki öneri).
- Bugünkü kod ekranı (*"Paste the activation link your workplace sent you."* — `mobile/ios/Taptime/ActivationCopy.swift:19`)
  girişi kazanır → adres alanı → **tek sabit ekran** (öneri metni: *"If this address belongs to an account, a link is on its
  way. It works for <TTL>. No email after a few minutes? Ask your manager for an invite."*) — sunucunun cevabından bağımsız;
  429'da *"Too many tries from this network — wait a few minutes or use mobile data."* (M16-35'in NAT bedeli) → link
  geldiğinde bugünkü yapıştırma yolu (iOS `InviteCode`, `ActivationCopy.swift:160-184`; Android `activation/Inputs.kt:9`).
- **Linkin uygulamaya yolu:** `/activate` Universal Link / App Link'e bağlı değildir (Q40, M16-08); e-postadaki link aynı
  telefonda tarayıcıyı açar ve kişi orada da tamamlayabilir (M16-35: tamamlanma iki yolda). Uygulamayı isteyen için ekran
  *"or copy the link and paste it here"* der (ADR 0049 §4 bedel 1).
- Adres cihazda saklanmaz ve loglanmaz. [G36-a] Android, [G36-b] iOS: sahte logger ile adresin hiçbir log çağrısına girmediği;
  R7M'nin `e_?mail` tetikleyicisi zaten vardır (`redline-check.sh:2642`).
- **Tarayıcıya dokunulmaz** — Q45 (b), istek yalnız uygulamada (kullanıcı, 2026-11-19). Reddedildi: Q45 (a) Faz G'de tarayıcı
  girişi (kullanıcı, 2026-11-19); tarayıcı isteği Faz G dışı ayrı karttır (*Sonra*). Tamamlanma tarayıcıda da olur (M16-35).
- İki platform tek metin listesi.

## M16-37 — Hesabı silme talebi: sunucu ve işverene iletim

- **Bağımlılık:** M16-35 (owner uyarı yolu) · Q13 (bağlam; bloklamaz)
- **Kırmızı çizgi:** §4.3 (`transactions` silinmez), §4.5, §4.6, §7
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(app): let an employee ask to delete their account`

**Amaç.** Apple 5.1.1(v)'nin uygulama içi silmesini, kayıtların hukuki delil olduğu bir üründe dürüstçe karşılamak.

**Kabul kriterleri.**
- **Uç:** `POST /app/v1/account/delete`, **nonce'lu imza**, gövde boş ya da `{}`. Tek transaction'da: kişinin bütün
  tarayıcı oturumları ve uygulama cihazları (+ canlı nonce'ları) iptal — `tenant.Staff.SignOutPhones`'ın kilit sırasıyla
  (oturumlar → cihazlar → nonce'lar; `internal/domain/tenant/staffsignout.go:125`) — ve bir `audit_log` satırı: action
  `employee.deletion_requested`, `detail` `{"app_device_id":…,"revoked_sessions":n,"revoked_devices":n}` (adres yok).
  Yanıt 204. Tekrar istek: cihaz artık iptal → 401 (M16-07).
- **Silinmeyenler:** `transactions` (§4.3), `punch_claims`, `audit_log`, `employee_invites`; `employees` satırı ne silinir ne
  deaktive edilir (ADR 0010). Anonimleştirme Q13'ündür. **Talep geri alınabilir:** aktif ve adresli bir çalışan e-postayla
  giriş açık bir işletmede yeniden aktive olabilir — bu bir *"her yerde çıkış + işverene haber"*dir; ekran söyler (M16-38).
- **Deaktive kişi:** T59'un kuralı (`tenant.ErrSignOutDeactivated`) — kimlik bilgisi **iptal edilmez**; audit satırı ve
  işverene iletim yine yapılır; yanıt 204.
- **İşverene iletim — Q46 (b), panel + owner e-postası** (kullanıcı, 2026-11-19): audit satırı + çalışan kartında *"Asked to
  delete their account on <date>"* (audit satırından; yeni sqlc sorgusu, `action = 'employee.deletion_requested'` ve `target` =
  çalışan, açık `tenant_id`) + M6-11 anomali ekranında son 90 günün listesi **ve** M16-35'in owner uyarı yolu (arka plan işi `ownerNotices`, `kind = deletion_request` — audit eylemleri
  `employee.deletion_notice.sent` / `.undelivered`, M16-35'in `kind` satırı;
  `Breaker.SendExempt`, owner başına günde 20 — aynı `httpx.Limiter` anahtar ailesinin kendi öneki): konu `An employee asked to
  delete their Taptime account`; gövde (anlamı): *"<Employee> asked to delete their account on <date>. Their phones are signed
  out. Their clock-in records are kept: they are legal records you control. Decide what to do with their data and reply to them
  within one month (GDPR Art. 12(3))."* Metin kullanıcıya gösterilir. Reddedildi: Q46 (a) yalnız panel (kullanıcı, 2026-11-19).
- **`GET /app/v1/me`'ye `businessName`** (M16-38'in *"işveren"* satırı): bugün `brand.name` isteğe bağlıdır
  (`apptap.go:76-81`); `"businessName": string` her zaman dolu. [G37-e] sözleşme şekli.
- *"Remove this phone"* ayrı kalır: `POST /app/v1/revoke` (`apptap.go:427`) yalnız **bu** cihazı kapatır, işverene bir şey
  iletmez.
- **Testler.** [G37-a] aktif kişi: oturumlar + cihazlar iptal, `transactions` sayısı aynı, tam bir audit satırı. [G37-b]
  deaktive kişi: hiçbir şey iptal edilmez (`revoked_at IS NULL`), audit satırı var. [G37-c] başka tenant'ın cihazıyla imzalı
  istek kendi tenant'ında kalır. [G37-d] owner e-postası sahte kesici reddederken de gider, adres gövdede yok.

## M16-41 — Gizlilik bildirimi: uygulama paragrafı ve yayım (M16-38'den önce)

- **Bağımlılık:** M16-37 (silme talebinin anlamı) · **kullanıcı + hukukçu**
- **Kırmızı çizgi:** — (GDPR Art. 13; mağaza beyanı yasal beyandır)
- **Araç:** yapıcı alt ajan (metin taslağı, CLAUDE.md §10) + kullanıcı (onay ve yayım) · **Mercek: tek**
- **Commit:** `docs(legal): describe the staff app before the store`

**Amaç.** M16-38'in gizlilik linki yayımlanmış bir metne gitmeli, oysa metni Faz F'deki M16-27 yazıyordu. Metin yarısı buraya
alınır; M16-27 mağaza etiketleri ve pazarlama cümleleriyle kalır.

**Kabul kriterleri.**
- **Bugün (kod okundu):** `/legal/privacy` rotası vardır (`web/templates/pages/landingview.go:1479`) ve yayımlanmış metin
  yokken yer tutucuyu basar — işaret *"Not in force yet"* (`web/templates/pages/legal_templ.go:337`) ve bölüm *"This text has
  not been published yet"* (`:556`). Denetçinin ölçümü (2026-11-18): canlıda yer tutucu.
- `docs/legal/privacy-draft.md`'ye başlığı **`The Taptime staff app`** olan bir bölüm: toplanan (cihaz açık anahtarı, kaba
  cihaz etiketi, tap anında konum, sahte konum bayrağı, attestation sonucu — Faz E), **iş e-postası** (yalnız eşleşme ve
  gönderim; cihazda ve log'da yok — ADR 0049), **Saatlerim** (işletme açtıysa ve seçtiği pencere kadar; koordinat, IP, güven
  puanı yok), **hesap silme talebi** (ne kapanır, ne kalır, işverene nasıl gider — panel + owner e-postası, Q46 (b)); **toplanmayan** (biyometri, arka plan
  konum, reklam kimliği, analitik).
- Hukukçunun onayı ve yayım **kullanıcıdadır** (operatör `/operator/legal`). Kabul: canlıda `curl -s https://taptime.mt/legal/privacy`
  çıktısında *"Not in force yet"* **yok** ve *"The Taptime staff app"* başlığı **var** (kullanıcı ya da ana oturum ölçer).
- **Bu kart mağaza gönderimini de bloklar** (her iki mağaza gizlilik politikası URL'si ister).

## M16-38 — Ayarlar ve gizlilik ekranı — iOS + Android

- **Bağımlılık:** M16-33 (ana ekrandan giriş), M16-37, **M16-41** (yayımlanmış gizlilik bildirimi)
- **Kırmızı çizgi:** §9 (yeni çalışan ekranı), §7
- **Araç:** iki yapıcı paralel + skill `tappa-brand` · **Mercek: tek** (sunucu yarısı M16-37'nin iki merceğinden geçti)
- **Commit:** `feat(mobile): add settings, privacy and account deletion`

**Kabul kriterleri.**
- Yerleşim M16-33'ün §9 sorusunda onaylıdır (madde 5).
- **Ad ve işveren** `/app/v1/me`'den (`employeeName`, M16-37'nin `businessName`'i).
- **"Remove this phone"** → `POST /app/v1/revoke` → yerel anahtar silinir → kod ekranı. iOS'ta istemci işlevi vardır ama
  ekran çağırmaz (`mobile/ios/Taptime/AppClient.swift:314`); Android'de yalnız yol sabiti vardır
  (`mobile/android/app/src/main/kotlin/mt/taptime/app/wire/Wire.kt:19`) — istemci işlevi bu kartta.
- **Privacy** → `https://taptime.mt/legal/privacy`, sistem tarayıcısında.
- **"Delete my account"** → onay ekranı (anlamı): *"This signs you out on every phone and removes this phone's key. Your
  clock-in records are not deleted: they are legal records <Business> keeps, and your request goes to them. If you still
  work there, you can sign in again later."* → `POST /app/v1/account/delete` → yerel anahtar silinir → kod ekranı. Deaktive
  kişide ilk cümle yerine *"Your request goes to <Business>."* (sunucu iptal etmez — M16-37).
- **Testler.** [G38-a] Android, [G38-b] iOS: silme ve çıkarma sonrası anahtar deposunda girdi yok (Keystore takma adı /
  Keychain sorgusu); [G38-c]/[G38-d] sahte logger ile ad ve işletme adının log çağrısına girmediği (M16-33'ün R7M eki
  `employeeName`'i de kapsar).

## M16-39 — App Review hazırlığı: inceleme notu, basılabilir QR, inceleyicinin aktivasyonu

- **Bağımlılık:** M16-22 · **M16-23** ve **M16-20** (Q47 (a) ve yedeği (c)) · M16-33, M16-36, M16-38 · kullanıcı ön koşulları
  (*"Taptime Demo"* açık; demo plaketi kodlanmış ve atanmış)
- **Kırmızı çizgi:** §4.3 (demo kayıtları da değişmezdir), depo PUBLIC (notta değer yok)
- **Araç:** **yapıcı alt ajan** `mobile/README.md`'yi ve kart düzeltmesini yazar (CLAUDE.md §10); kullanıcı üretimde demo
  işletmeyi kurar, App Store Connect / Play Console'u doldurur ve Play'in silme formunu okur; ana oturum yalnız salt okuma
  doğrulaması ve AskUserQuestion · **Mercek: tek**
- **Commit:** `docs(mobile): prepare the app for store review`

**Amaç.** Daveti ve plaketi olmayan bir inceleyicinin uygulamayı uçtan uca kullanabildiği, gerçek ürün yüzeylerinden
kurulmuş, tekrarlanabilir bir inceleme düzeni (kullanıcı kararı 3; 2.1(a)).

**Kabul kriterleri.**
- **Demo işletmenin durumu** (kullanıcının kurduğu; yapıcı salt okuma çıktısından kart düzeltmesine yazar): bir lokasyon;
  Saatlerim **açık**, 4 hafta (M16-30); e-postayla giriş açık (M16-35'in config PR'ı + owner ayarı); **iki** demo
  çalışan (M16-40'ın iki akışı için — bekleme süresi aynı kişiyi 7 gün kilitler); demo çalışanların adresleri **panel
  girişinin adresinden farklı** (`ErrAddressIsAdministrators`, `internal/invite/email.go:100`, `:447`) ve kullanıcının
  denetimindeki posta kutuları; bir **manager** rolünde panel hesabı.
- **Manager'ın yapabildikleri — düzeltilmiş ve kabul edilmiş bedel (denetim L2).** Davet politika motorundan geçmez
  (`base:authz-manager` eylem listesinde davet yoktur, `internal/policy/baseline.go:541-551`, çünkü davet orada değildir);
  personel ekleme ve davet gönderme rol sınamaz (`internal/handler/dashboard.go:433-434`, `employeeactions.go:220-283`,
  `:396-450`); yalnız ekranda link gösterme yedeği owner'a özeldir (`internal/handler/inviteemail.go:351`). Yani demo
  işletmenin manager girişini alan inceleyici **keyfî adreslere DKIM imzalı davet e-postası** gönderebilir (işletme başına
  50/sa, ADR 0022 §9), kişileri **geri alınamaz biçimde deaktive** edebilir (ADR 0010), `flag`'leri onaylayabilir, manuel
  kayıt girebilir ve rapor alabilir. Bu, inceleme notunda panel girişi vermenin **adlandırılmış bedelidir**; azaltma: her
  incelemeden sonra *"kadroyu denetle"* adımı (aşağıda) ve parola değişimi.
- **İzinlerin ölçümü (denetim A2-16):** M16-23'ün müdür onayı ve M16-20'nin ekran eşlemesi manager'a açık mı — iki kart bunu
  söylemez, `base:authz-manager` ikisini de içermez. Bu kart iki uçla, **yerelde kurulmuş eşdeğer bir manager hesabıyla**
  ölçer (demo işletme yalnız üretimdedir; üretimde yazma denemesi yapılmaz) ve sonucu yazar: **onay açık** → Q47 (a); **onay kapalı, eşleme açık** → (c); **ikisi de kapalı** → notta
  owner girişi gerekir — bu, yukarıdaki bedeli owner'ın tam yetkisine genişletir ve **kullanıcıya sorulur** (AskUserQuestion)
  ya da M16-23/M16-20'ye manager yetkisi eklenir (o kartın kart düzeltmesi + `base:authz-manager` değişikliği, kullanıcı
  onayı).
- **Demo plaketi:** statik QR bir plaket UID'si ister (`sun.PlaqueQRURL`, `internal/sun/mint.go:81`) ve üretimde plaketsiz QR
  yoktur (sanal plaketler yalnız sandbox'ta — `internal/db/virtualtags.go`). Atama kodlanmış plaket ister (`db/queries/tags.sql:426`).
  Plaket **kalıcı olarak** demo işletmenindir; `tags` satırı hiçbir temizlikte silinmez. QR'ın basılabilir sayfası App Store
  Connect ekine konur.
- **İnceleyicinin aktivasyonu — Q47 (a), (c) notta yedek** (kullanıcı, 2026-11-19): (a) inceleyici uygulamada *"My phone can't tap"* der, manager girişiyle panelde eşleşme koduyla onaylar
  (M16-23). M16-23'ün öz-onay kuralı bir karşılaştırmadır (onaylayanın `identity_id`'si ile çalışanın kayıtlı adresinin
  kimliği aynıysa ret); demo düzeninde adresler farklıdır. (c) inceleyici panelde kendi tarayıcısında bir canlı QR ekranı
  eşler (M16-20) ve uygulamayla okur. ⚠️ **M16-23, lokasyonda aktif bir canlı QR ekranı varken müdür onayını sunmaz** — (c)'den
  kalan bir ekran bir sonraki incelemede (a)'yı kapatır. Runbook bu yüzden her incelemeden sonra ekranı **eşlemeden
  çıkarır**.
- **İnceleme notu şablonu** — yeni dosya **`mobile/README.md`** (bugün yalnız `mobile/android/README.md` vardır). Bölüm
  *"Store review"*, yalnız yer tutucularla (`<INVITE_LINK>`, `<PANEL_EMAIL>`, `<PANEL_PASSWORD>`, `<QR_ATTACHMENT>`); gerçek
  değerler yalnız App Store Connect *App Review Information* ve Play Console *App access*'e. İçerik: uygulamanın kim için olduğu
  (3.1.3(c)), adım adım yol (link yapıştır → onay → aktivasyon (Q47) → QR'ı okut → FLAGGED kaydı → Saatlerim → Ayarlar →
  *Delete my account*), NFC'nin gerçek kullanımda plaketle olduğu ve QR'ın neden verildiği (2.1(a)), Sign in with Apple'ın
  neden yok (4.8). **Ölçü:** `git grep -nE '@|https?://[^<]' mobile/README.md` yalnız `taptime.mt` ve yer tutucu satırlarını
  verir; *"Rejection risks"* tablosu 2.1(a), 4.2, 5.1.1(v), 3.1.3(c), 4.8 ve **Play'in web silme yolu** satırlarını içerir.
  Sonuncusu **ölçülmemiş bir mağaza riskidir**: kullanıcı Play Console'un *Data safety → account deletion* formunu gönderimden
  önce okur ve cevabı karta yazar; web yolu istenirse cevap M16-37'nin işverene iletimidir.
- **Runbook (her inceleme döngüsü):** önce — taze davet (tek kullanımlık, en çok 30 gün: `internal/invite/manager.go:135`),
  QR eki; sonra — **kadroyu denetle** (salt okuma: demo işletmede inceleme sırasında eklenen kişi, gönderilen davet, deaktivasyon,
  onay ve manuel kayıt `audit_log`'dan listelenir; beklenmeyen bir şey kullanıcıya), canlı QR ekranını eşlemeden çıkar, demo
  cihazlarını panelden çıkar, **panel parolasını değiştir**.
- **Faturalama/sayım:** demo işaretleyici yoktur (`git grep -nE 'is_demo|is_test|billing_exempt'` → 0). Tahsilat elledir (Q24):
  kullanıcı demo işletme için fatura kesmez; operatör sayımlarında görünmesi kabul edilmiş bedeldir (işaretleyici *Sonra*).
- **Söküm:** demo artık gerekmediğinde operatör işletmeyi **askıya alır** (ADR 0025); silinmez, plaketin `tags` satırı kalır.
- Uygulama içi demo modu **yoktur** (kullanıcı kararı 3).

## M16-40 — Faz G kapanışı

- **Bağımlılık:** M16-30 … M16-39, M16-41
- **Araç:** yapıcı (kullanıcıyla cihaz başında) + üçüncü göz · **Mercek: iki**
- **Commit:** `test(mobile): close the staff self-service phase`

**Amaç.** Faz G'yi gerçek cihazlarla ölçmek ve fazın büyük testlerini bir kez koşmak.

**Kabul kriterleri.**
- **Ortam:** üretim, **yalnız *"Taptime Demo"*** (KF/KM'ye hiçbir yazma yok). Ön koşul: M16-35'in config PR'ı demo UUID'siyle
  canlı; üretimin e-postası açık (`deploy/k8s/05-config.yaml:166`); posta kutuları kullanıcının.
- **Tur** (gerçek bir iPhone ve bir Android): Saatlerim ayarı (owner kapatır → giriş kaybolur, imzalı uç 404; 1 haftaya indirir
  → geçen hafta yanıtta yok) · Saatlerim (açık giriş → `complete:false`; kararı bekleyen `flag` ayrı; QR dokunuşu `waiting`;
  Faz D'nin canlı QR dokunuşu `channel:"qr_live"`) · ayarlar · *Remove this phone* (sonra plaket dokunuşu → kayıt yok,
  `transactions` sayısı değişmez, tarayıcı yolundaysa bir `tap.signed_out_phone` audit satırı) · *Delete my account* (kimlik
  bilgileri kapandı, `transactions` aynen, panel satırı ve owner e-postası geldi — Q46 (b)) · deaktivasyon adımı yalnız bu tur için açılan **tek
  kullanımlık** bir test çalışanıyla (geri alınamaz — ADR 0010).
- **E-postayla giriş — iki demo çalışan** (bekleme süresi aynı kişinin ikinci self aktivasyonunu 7 gün sessizce reddeder):
  - **Çalışan 1 — uçtan uca:** bilinmeyen adresle aynı ekran ve **hiç** e-posta; çalışan 1'in adresiyle link → uygulamada
    plaket → aktive; owner uyarısı geldi; anomali listesinde satır. Aynı link akışının **tarayıcı** tamamlanması ayrı bir
    turda değil, [G35-i]'nin DB testindedir.
  - **Çalışan 2 — 24 saatlik TTL vakası (Q44 (a)), sırası önemli** (denetim N10): önce **link B** istenir ve **kullanılmaz**; süresi
    geçince `lookup`'ta reddedildiği ölçülür. **Sonra link A** istenir ve tamamlanır. Ters sıra bozardı: A'nın tüketimi bekleme
    süresini başlatır (B'nin isteği sessizce reddedilirdi) ve her yeni basım bekleyen `self` davetini iptal eder
    (`CancelPendingInvitesForEmployee`, `internal/invite/manager.go:312`). Kişi bütçesi (1/sa, 3/gün) iki isteğe yeter. B'nin reddi 25.
    saatte; A akşam istenir ve ertesi gün, 24 saatin içinde plakette tamamlanır.
  Kayıt yazan adım DB satırıyla, yazmayan adım sayının değişmediğiyle kanıtlanır (salt okuma, kullanıcı ya da ana oturum).
- **Demo durumunu geri yükle (denetim A2-18):** tur bitince owner Saatlerim'i **açık, 4 hafta**, e-postayla girişi **açık**
  bırakır, canlı QR ekranını eşlemeden çıkarır, test cihazlarını panelden çıkarır. **Ölçü:** salt okuma
  `staff_hours_weeks = 4` ve `email_signin = true` demo işletmenin satırında; `/app/v1/me`'nin `hoursWeeks` = 4.
- **İnceleyici provası:** **kullanıcı** (ya da kullanıcının adlandırdığı, notu yazmamış biri) `mobile/README.md`'nin notunu
  sıfır kurulu bir telefonda harfiyen izler; takıldığı her adım nota düzeltme olarak döner. Prova sonunda geri yükleme adımı
  yeniden koşar.
- Fazın büyük testleri bir kez: tam `-race`, `go test ./...`, `make check`, `make audit`, iki platformun test paketleri.
- ADR 0049'a *"Uygulandı"* notu; Apple bölüm numaralarının güncel metne uyduğu okunur.
- **M9-05'i bu kart kapatır:** `state.md` M9-05 satırı *"M16-30…33 ile karşılandı (TARİH)"*, durum `done`; Faz G done.

---

# Faz F — Yayın + pilot

## M16-27 — Gizlilik: `/legal/privacy`, mağaza etiketleri

- **Bağımlılık:** M16-26 · **M16-40 (Faz G — kullanıcı kararı 2026-11-18: Faz G yayından önce)** · **M16-41** (bildirimin
  metni ve yayımı — Faz G'de) · Q42 · kullanıcı/hukukçu onayı
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `docs(legal): describe the staff app`

**Amaç.** İki mağazanın gizlilik beyanını yayımlanmış bildirime (M16-41) eşitlemek ve pazarlama cümlelerini Q42'ye göre
yazmak. **Denetim düzeltmesi (2026-11-18, M4):** `docs/legal/` uygulama paragrafı ve yayımı bu karttan **M16-41'e**
taşındı — M16-38'in gizlilik linki Faz F'den önce yayımlanmış bir metne gitmeli. Bu kart Faz E'nin attestation satırı
M16-41'den sonra değiştiyse paragrafı günceller.

**Kabul kriterleri.**
- App Store gizlilik etiketi ve Play *Data safety* formunun cevap taslağı, her satır bir kod yerine bağlı: cihaz anahtarı,
  tap anında konum, sahte konum bayrağı, attestation (Faz E); **iş e-postası** *uygulama işlevi* amacıyla, kimliğe bağlı,
  izleme yok (ADR 0049); Saatlerim'de gösterilen kendi kayıtları (M16-30/M16-31); hesap silme talebi (M16-37). Play'in
  *account deletion* sorusunun cevabı M16-39'un *"Rejection risks"* satırının ölçümüyle yazılır.
- Pazarlama cümleleri Q42'nin cevabına göre (`web/templates/pages/*view.go` olgu sistemi, `FactTapPageNeedsNoApp`);
  `docs/handoff.md:27` (slogan), `:54-55` (Connecteam karşılaştırması) ve `:60` (*"app-less akış"* farkı) da Q42'nin
  cevabıyla bu kartta güncellenir ya da *"kalsın"* ise aynen bırakıldığı yazılır (M16-01 onlara dokunmadı).

## M16-28 — İmza, iç test dağıtımı, sürüm süreci

- **Bağımlılık:** M16-27 (dolayısıyla Faz G, M16-40) · M16-39 (inceleme notu — App Store Connect'in dış beta
  incelemesi de onu ister) · kullanıcı: mağaza hesapları, imza anahtarı saklama yeri
- **Kırmızı çizgi:** §4.7 (imza anahtarları repoda yok)
- **Araç:** yapıcı + kullanıcı · **Mercek: iki**
- **Commit:** `docs(mobile): document signing and internal releases`

**Amaç.** İmza anahtarlarını repo dışında tutarak iki mağazada iç test dağıtımını ve geri alınabilir bir sürüm sürecini
kurmak.

**Kabul kriterleri.**
- Android: Play App Signing; yükleme anahtarı repoda değil (Infisical); `assetlinks.json` parmak izi Play App
  Signing sertifikasınınki (M16-08 env'i güncellenir).
- **Üretimin `TAPPA_APP_ANDROID_SIGNER_SHA256`'sı yalnız Play App Signing özetini taşır;** yükleme ve iç test
  anahtarlarının özeti yalnız test ortamlarının env'ine yazılır (listedeki her anahtar plaket URL'sini açan bir
  uygulama imzalayabilir; yükleme anahtarı geliştirici makinesindedir — M16-08 güvenlik denetimi, 2026-11-14).
- iOS: App Store Connect iç testi; sertifika ve profiller repoda değil.
- `redline-check` R7d imza dosyası kalıplarını (`*.jks`, `*.keystore`, `*.p12`, `*.mobileprovision`) tanır.
- Sürüm runbook'u `mobile/README.md`'de: sürüm numarası, derleme, yükleme, geri alma (eski sürüm sunucuyla uyumlu
  kalır — `/app/v1` geriye uyumluluk kuralı yazılır).

## M16-29 — KF/KM pilotu ve M16 kapanışı

- **Bağımlılık:** M16-28 · M8-07 envanteri
- **Araç:** ana oturum + kullanıcı · **Mercek: tek**
- **Commit:** `docs(plan): close M16`

**Amaç.** Uygulamayı ve NFC'siz yolları KF/KM'de gerçek kadroyla ölçmek ve milestone'u kapatmak.

**Kabul kriterleri.**
- Pilot ölçüleri (salt okuma): P6 çalışanlarının `flag` oranı önce/sonra · uygulama benimseme oranı · `qr_live`
  kullanımı · müdür onaylı aktivasyon sayısı · sahte konum bildirimleri.
- ADR 0045 *"Uygulandı"*; A3, P6, Q11 satırlarının durumu ölçüyle güncellenir; ADR 0005 risk tablosu son hâli.
- **ADR 0005'e ek not** (append kuralı, satır silinmez): risk 1'in *"tek çözümü biyometri veya uygulama kurulumu —
  ikisi de ürünün varlık sebebine aykırı"* gerekçesi (`docs/adr/0005-kabul-edilen-riskler.md:74`) ve risk 2'nin
  *"attestation uygulama ister (app-less vaadi)"* gerekçesi (`:75`) isteğe bağlı uygulamayla yeniden okunur: uygulama
  var ama buddy punching'i çözmez (cihaz anahtarı devretmeyi durdurmaz), attestation yalnız kanıttır (ADR 0045 §2.7).
  Aynı ekle canlı QR aktarmasının iki kalıcı biçimi kabul edilen risk olarak girer (ADR 0045 Bedel 5): kopyalanan ekran
  oturumu çerezi / yanlış eşlenmiş ekran ve canlı QR fotoğrafının bir davetliye iletilmesiyle uzaktan aktivasyon.
  `TestADR0005_TheAnchorCountsMatchTheProse` ve `TestADR0005_TheRiskCountMatchesTheTable` yeşil.
- `state.md`: M16 bitti.

---

## Sonra (numarasız — kart açılınca sıradaki numarayı alır)

- **Çevrimdışı kuyruk** (M9-01 ile birlikte): kuyruktaki dokunuş sunucu saatini alır (ADR 0038), `base:queued-window`
  eşiği ötesi `flag`; imzalı istek + nonce kuyrukta nasıl yaşar ayrı karar (nonce süresi dolar). Ayrı ADR.
- **Unutulan çıkış bildirimi — konumsuz:** saat ve açık giriş üzerinden yerel/push bildirim; §4.2 gereği konum
  tetikleyicisi yok. (Kullanıcının 2026-11-18 listesindeki *"unutulan çıkış hatırlatması"* budur — ayrı madde değil.)
- **"Çıkışı unuttum" düzeltme talebi Saatlerim'den** (2026-11-18): açık kaydın yanından ADR 0032 Faz C'nin talebi
  (`punch_claims`; saati yalnız müdürün onayı yazar). Bugün talep yalnız sonuç ekranındaki karttır (K4: en çok iki
  gösterim); Saatlerim'den gönderim yeni bir giriş noktasıdır — §9 ve K4 ile birlikte karar.
- ~~**"Saatlerim" ekranı** (M9-05'in uygulama yarısı).~~ → **Faz G'ye alındı** (M16-30 … M16-33, kullanıcı kararı
  2026-11-18).
- Hesap silme talebinin **tarayıcı** eşi (M16-37 yalnız uygulamayı kapsar).
- Hesap silme **talep tablosu + panelde *"silme talepleri"* kuyruğu** (denetim turunda Q46'dan çıkarıldı) — Q13'ün
  anonimleştirme akışıyla birlikte.
- E-postayla giriş için **kısa kod** (6–8 hane, yeni tablo, deneme kilidi) ya da **uygulamaya bağlı ayrı link yolu**
  (App Link / Universal Link; `/activate` değişmez) — denetim turunda Q44'ten çıkarıldı; pilot kopyala-yapıştır sürtünmesi
  gösterirse.
- E-postayla girişin **tarayıcı** isteği (Q45 (b), kullanıcı 2026-11-19 — ayrı kart; kapısı canlı *"Finish setup"* teşhis
  logunun verisidir, `455fc561`).
- Saatlerim tarayıcı sayfasına **link** (*"Activation complete"* ekranında ya da davet e-postasında) — korumalı §9 ekranı ve
  ayrı şablon değişikliği; M16-32 linksiz çıkar.
- Demo/test işletme **işaretleyicisi** (faturalama ve sayımdan düşmek için; bugün yok — M16-39).
- Attestation'ı karar verici yapmak — ayrı ADR.
