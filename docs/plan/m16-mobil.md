# M16 — Personel mobil uygulaması (isteğe bağlı) + NFC'siz telefonlar

> **Durum:** plan **onaylandı** — yedi bağımsız denetim turundan sonra ONAY (`407710bf`, 2026-11-13). **ADR 0044 kabul
> edildi (2026-11-14, M16-01).** `main`'e birleştirilmedi. Kod yok. Canlı durum yalnız [state.md](state.md)'dedir;
> bu dosyaya "yapıldı" yazılmaz.
>
> **Karar belgesi:** [ADR 0044](../adr/0044-istege-bagli-native-uygulama.md) (durum *Kabul edildi (2026-11-14)*).
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
> **D, E, F**.

## Kullanıcı kararları (2026-11-12/13) — bağlayıcı

Yalnız bu beş madde kullanıcının **açık** kararıdır; yeniden tartışılmaz (ADR 0044 §2'de *"karar"* diye işaretli):

- Native uygulama **isteğe bağlı**; tarayıcı akışı kalır ve tam desteklenir.
- **Tamamen native:** iOS Swift/SwiftUI, Android Kotlin/Jetpack Compose — Node içeren yol (React Native / Expo)
  dışarıda (CLAUDE.md §1).
- NFC'siz telefon için **statik QR ve canlı QR birlikte**.
- NFC'siz aktivasyon: lokasyonda **canlı QR ekranı varsa onunla, yoksa müdür onayıyla**.
- **Attestation** Faz E'de **yalnız kanıt + rapor**; güveni ve verdict'i değiştirmez.

## ADR kabulüyle kesinleşen kararlar (2026-11-14)

> **Kullanıcı 2026-11-14'te ADR 0044'ü olduğu gibi kabul etti; aşağıdakiler o kabulle karardır** (ADR 0044 *"Kabul
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
| Kanal sözlüğü kapalı | `00005:114`, `00043:141`, `00046:137`, `00048:171`, `00067:193`, `00095:66` | ⚠️ **10 migration + 2 sorgu + Go'da çok sayıda karşılaştırma** (liste bir başlangıçtır; kanıt M16-21'in parite testi) — `qr_live` geniş bir değişiklik (ADR 0044 Bedel 6) |
| Akış `qr_live`'ı görür mü | `00067:193`, `checkin.go:1687` (`feedEligible`), `openapi.yaml:1105` | ❌ bugün **sessizce atlar**; `/v1` enum'u kapalı → Q38 |
| Canlı QR varsayılanı yeni baseline ister mi | `baseline.go:287-309` | ⚠️ **istemeyebilir:** yalnız `base:qr-requires-ip` kanal koşullu (`channel == "qr"`) |
| Tazelik ve SUN guardrail'leri | `guardrails.go:396`, `:479` | NFC-only → `qr_live` için karar gerekir (M16-21) |
| Debounce'un sunucu saati bacağı | `db/queries/transactions.sql:317` (`SecondsSinceLastRecordedTap`) | ❌ `channel IN ('nfc', 'qr')` → `qr_live` girmezse geriye tarihlenmiş bir `qr_live` dokunuşu debounce'u **sessizce** atlar (M16-19, M16-21) |
| Motor her tap'te plaket varsayar | `internal/domain/tap/types.go:471-473` (`Tag` *"Always present for a resolved tap"*), `/v1` `tagId` (`openapi.yaml:1130-1133`) | ❌ `qr_live` satırının plaketi yok → M16-21 |
| Tenant bilinmeden çözümleme | `00003` `resolve_session_by_token_hash` (`tappa_resolver`, SECURITY DEFINER, sabit `search_path`) | cihaz, ekran oturumu ve eşleşme kodu aynı desenle; dokunuş anındaki ekran `00004` `resolve_tag_by_uid` deseniyle çözülür (M16-03, M16-19) |
| Yeni baseline belgesi var olan tenant'a nasıl girer | `checkin/policyset.go:422`, `:529` | ilk dokunuşta **onaysız** materialize edilir (ADR 0044 Bedel 10) |
| `transactions` INSERT yetkisi | ADR 0038 | **sütun listesi** → `client` için ayrı GRANT |
| İkinci cihaz ilkini iptal eder | `activate.go:1068-1077` | ✅ → uygulamaya genişler (Q40) |
| Panelin *"telefonlarını çıkar"* eylemi | `employeeactions.go:690` (T59) | ✅ var → uygulama cihazlarını da kapsamalı (M16-09) |
| Oturum paketi attestation yapmaz | `internal/session/manager.go:18-22` | ✅ → cihaz doğrulaması **ayrı pakette** |
| Repoda Android var mı | `android/` (ADR 0017 encode rölesi, `mt.taptime.relay`) | ⚠️ **başka uygulama**; yerinde kalır, toolchain ölçüsü oradan alınır (`android/README.md`) |
| Kırmızı çizgi taraması mobil kodu görür mü | `scripts/redline-check.sh:33` (`SRC`), `:2530` (`B1_SRC`) | ❌ `mobile/` yok → M16-02 kod kartlarından **önce** |
| CI mobil derler mi | `.github/workflows/*.yml` | ❌ hayır → Q36 (d) |
| Son migration / ADR | `db/migrations/00099_*`, `docs/adr/0043-*` | ✅ sıradaki 00100, ADR 0044 (state.md Deploy 13 notuyla aynı) |

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı** bir denetçi kabul
  kriterlerini tek tek doğrular; bulgu → düzeltme → **yeni** denetçi. **"Mercek: iki"** = ayrıca agent
  `tappa-security-auditor` (şema, karar motoru, güvenlik sınırı). **"Mercek: tek"** = yalnız üçüncü göz.
- **Büyük testler yalnız FAZ SONUNDA** (kullanıcı kuralı, 2026-10-08/09: *"her adımda test yapıp çok zaman
  kaybetme"*). Tur içinde yalnız değişen testler `-run` ile + saniyelik kapılar (`gofmt`, build, `bash
  scripts/redline-check.sh`). Tam `-race`, `go test ./...`, `make check`, mobil test paketlerinin tamamı ve gerçek
  cihaz turu **faz kapanış kartında bir kez** (M16-10, M16-14, M16-18, M16-24, M16-26, M16-29). Bu cümle her yapıcı
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

`main`'in son migration'ı **00099**, son ADR'si **0043** (`0040` hiç kullanılmadı). Aşağıdaki dağıtım **öneridir**;
her kart açılırken `main`'in o anki son numarası **yeniden okunur** ve numara oradan alınır (başka dallar araya
girebilir; goose, uygulanmış en yüksek sürümün altındaki bir dosyayı sonradan *missing* sayar — M15'in 2026-10-22
numara değişikliği notu).

| No (öneri) | Kart | İçerik | `Down` |
|---|---|---|---|
| 00100 | M16-03 | `app_devices` + `app_request_nonces` (§6 beşlisi, GRANT) + cihaz çözümleyicisi (SECURITY DEFINER, `tappa_resolver` — `00003` deseni) | satır varsa reddeder |
| 00101 | M16-04 | `transactions.client` + **tek** CHECK `client IS NULL OR client IN ('browser','app')` `NOT VALID` (NULL'u kabul eder — `00065` deseni) + sütun INSERT GRANT'ı (ADR 0038) | dolu değer varsa reddeder |
| 00102 | M16-04 | CHECK'in `VALIDATE`'i (`00044` deseni; NULL'lu tarihsel satırlar geçer) | `NOT VALID` hâline döner |
| 00103 | M16-19 | `live_qr_displays` (+ eşleşme kodu) + üç çözümleyici: ekran oturumu ve eşleşme kodu (`00003` deseni), dokunuş anındaki ekran → tenant/lokasyon/iptal (`00004` `resolve_tag_by_uid` deseni) | satır varsa reddeder |
| 00104 | M16-19 | `qr_live` kanal değeri — bütün kanal CHECK'leri ve akış tetikleyicisi (envanter M16-19'da) | `qr_live` satırı varsa reddeder |
| 00105 | M16-23 | müdür onaylı aktivasyon: bekleyen cihaz isteği (§6 beşlisi, GRANT) | satır varsa reddeder |
| 00106 | M16-25 | attestation sonucu (sütun ya da tablo — kart seçer; tabloysa §6 beşlisi, GRANT) | dolu değer varsa reddeder |

ADR: **0044** bu milestone'un karar belgesi. Attestation'ı karar verici yapmak ya da çevrimdışı kuyruk **ayrı ADR**dir.

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

## Kart listesi

| Faz | Kart | Başlık | Mercek |
|---|---|---|---|
| 0 | M16-01 | ADR 0044'ün kabulü ve CLAUDE.md / handoff metinleri | iki |
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
| F | M16-27 | Gizlilik: `/legal/privacy`, mağaza etiketleri | tek |
| F | M16-28 | İmza, iç test dağıtımı, sürüm süreci | iki |
| F | M16-29 | KF/KM pilotu ve M16 kapanışı | tek |

---

# Faz 0 — Karar

## M16-01 — ADR 0044'ün kabulü ve CLAUDE.md / handoff metinleri

- **Bağımlılık:** —
- **Kırmızı çizgi:** §4.1, §4.2, §4.5, §4.7 (ADR'nin analizi), §1 (stack), §5, §9
- **Araç:** ana oturum; kullanıcı onayı · **Mercek: iki** (ADR bir güvenlik sınırı tanımlar: taşıyıcı tokensız kimlik)
- **Commit:** `docs(adr): accept the optional native staff app`

**Amaç.** ADR 0044'ü *Kabul edildi*'ye geçirmek; Q35–Q42'yi sormak (cevap ya da hangi kartı blokladığı yazılır);
ürün ve stack cümlelerini kabul edilen karara eşitlemek.

**Bu kartta değişen cümleler** (satırlar `70e8a016`'ya göre; yapıcı yeniden okur):

| Dosya | Bugün | Olacak |
|---|---|---|
| `CLAUDE.md:6` | *"tarayıcı açılır (uygulama kurulumu YOK)"* | *"tarayıcı açılır (uygulama kurulumu **gerekmez**; isteğe bağlı native uygulama aynı URL'yi açar — ADR 0044)"* |
| `CLAUDE.md` §1 tablosu (`:15-24`) | mobil satırı yok | iki satır: *Mobil (isteğe bağlı)* — iOS Swift/SwiftUI, Android Kotlin/Compose, `mobile/`, Go modülü dışında, üçüncü parti SDK yok (ADR 0044); ve mevcut `android/` rölesinin ADR 0017 açılımı |
| `CLAUDE.md:26-27` | *"Node/npm/node_modules bu repoda ASLA yer almaz"* | **değişmez** — yanına: *"mobil derlemeler de Node'suzdur (RN/Expo bu yüzden reddedildi, ADR 0044)"* |
| `CLAUDE.md` §3 dizin haritası (`:50-150`) | `android/` ve `mobile/` yok | `android/` (encode rölesi, ADR 0017) · `mobile/ios/` · `mobile/android/` · `internal/appdevice/` (cihaz anahtarı doğrulaması — kart M16-05 yazınca) |
| `CLAUDE.md:158-159` (§4.1) | *"Telefonun kendi ekran kilidi bizim katmanımız değildir"* | aynen + *"uygulamada biyometri API'si ve biyometrik anahtar erişim denetimi yoktur (ADR 0044 §3)"* |
| `CLAUDE.md:160-161` (§4.2) | *"Arka plan konum, sürekli takip, geofence izleme YOK"* | aynen + *"uygulamada arka plan konum izni ve modu da yoktur"* |
| `CLAUDE.md` §3 `internal/sun/` satırı (`:76-77`) | *"Kriptografi SADECE burada"* | *"**Plaket** kriptografisi (AES-CMAC, SUN, `ctr`) SADECE burada"* — `internal/appdevice`'in ECDSA doğrulaması (M16-05) ve canlı QR token imzası (M16-20) bu cümleyle çelişmesin; yeni iki yer §3'e kendi satırıyla girer (`internal/appdevice/` M16-05, canlı QR paketi M16-20) |
| `CLAUDE.md` §7 *"Asla loglanmaz"* listesi (`:474-` paragrafı) | oturum token'ı, CMAC, AES anahtarı, davet kodu, e-posta, tam GPS; operatör ve dış API tarafı | listeye eklenir: **cihaz imzası, istek nonce'u, canlı QR token'ı, ekran eşleşme kodu, ekran oturumu çerezi, canlı QR imza anahtarı**, müdür onayı eşleşme kodu (hata mesajı ve `audit_log` `detail`'i dahil) |
| `CLAUDE.md` §11 tablosu (`:589-600`) | mobil satırı yok | *"iOS / Android personel uygulaması — `mobile/README.md` + ADR 0044"* |
| `docs/handoff.md:24` | *"tarayıcı otomatik açılır (uygulama kurulumu YOK)"* | *"… (uygulama kurulumu gerekmez; isteğe bağlı uygulama — ADR 0044)"* |
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
- ADR 0044 durum satırı *Kabul edildi (TARİH)*; Q35–Q42 her biri ya *cevaplandı (TARİH)* ya da *"açık — bloklar
  M16-xx"*.
- Yukarıdaki ilk tablonun her satırı uygulandı; ikinci tablonun satırları bu kartta **uygulanmadı** (diff'te §5/§9
  değişikliği yok).
- `go test -run 'TestEveryNamedTestExists|TestClaudeMD_TheIdentityExceptionIsADR0031sSection61' ./cmd/tappa/` yeşil
  (CLAUDE.md'ye dokunan kart CLAUDE.md'yi okuyan testi koşar).
- `state.md` M16 satırı: *"ADR 0044 kabul edildi, Faz A açık"*.

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
  `setUserAuthenticationRequired`. (Mevcut `face[_-]?id` / `touch[_-]?id` zaten bir kısmını yakalar — ölç, tekrarlama.)
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

**Tuzaklar.** Kalıplar sözcükseldir: `startMonitoring(for:` gibi bir yazım Swift'in başka API'sini de yakalayabilir —
muafiyet kuralı (R1'in `R1_WAIVER_PHRASES` deseni) gerekçesiz eklenmez.

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

## M16-04 — Şema: `transactions.client`

- **Bağımlılık:** M16-01
- **Kırmızı çizgi:** §4.3, §4.6, §6
- **Araç:** agent `tappa-db-migrator` · **Mercek: iki**
- **Commit:** `feat(db): record which client wrote a tap`

**Amaç.** Her yeni tap satırının hangi istemciden geldiğini değişmez olarak yazmak.

**Kabul kriterleri.**
- `transactions.client text NULL`, **tek** CHECK `client IS NULL OR client IN ('browser','app')` `NOT VALID`;
  tarihsel satırlar NULL, geri doldurma yok, `DEFAULT` yok.
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
- Kanonik metin tek fonksiyonda tanımlıdır (yöntem, yol, gövde özeti, nonce, cihaz kimliği — ADR 0044 §2.3) ve iki
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

## M16-06 — Policy bağlamı: `Client`, `MockLocation`; sahte konum baseline'ı

- **Bağımlılık:** M16-04
- **Kırmızı çizgi:** §5 (normatif), §4.6, CLAUDE.md §8 (%90+)
- **Araç:** yapıcı · **Mercek: iki**
- **Commit:** `feat(policy): review a tap whose phone reports a mock location`

**Amaç.** `tap.Input`'a `Client` ve `MockLocation`; bağlam anahtarları (öneri: `tap:client`, `tap:mockLocation` —
mevcut `tap:*` adlandırması, `internal/domain/tap/decide.go:93`); yeni baseline `base:mock-location-review` (ADR 0044
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
ADR 0044 Bedel 10). Uygulamasız tenant'ta sonuç değişmediği ölçülmeli: `MockLocation` yalnız `/app/v1` yolunda dolar.

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
- **İptal edilmiş cihazın dokunuşu §5 satır 3'ün ikizi:** 401 / yeniden aktivasyon, **kayıt yok**, tarayıcının
  `"tap page: a signed-out phone tapped"` uyarısının eşi bir WARN satırı (yalnız tenant, çalışan, kanal —
  `tap.go:446-454`) (DB testi: `transactions` sayısı değişmez).
- İptal tek yönlüdür (`00099` deseni; tetikleyici ya da yetki).
- Panel metni (§9 değil, panel): *"Sign out phones"* artık uygulamayı da kapsadığını söyler — metin değişikliği
  kullanıcıya gösterilir.

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
- ADR 0044'e *"Faz A uygulandı"* notu (migration numaraları, ölçülen sapmalar).
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
- *Doğrula* maddelerinin sonuçları ADR 0044'e not.

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
- Sonuç ekranı düğmesiz; *"tarayıcıda aç"* çıkışı yalnız hata/aktive değil ekranında (ADR 0044 Bedel 8; §9 sorusu).
- CLAUDE.md §9'a uygulama ekranlarının eşlik cümlesi (bu kartla ya da M16-13 ile — ilk biten).

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
- Fazın büyük testleri bir kez; *doğrula* sonuçları ADR 0044'e not.

---

# Faz D — NFC'siz telefonlar

## M16-19 — Şema: canlı QR ekranları ve `qr_live` kanal envanteri

- **Bağımlılık:** M16-10 · Q38, Q39
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
  (`directionSourceColumn`), `:1688` (`feedEligible`), `:1815` (NFC-only); `internal/domain/tap/directionsource.go:43`
  (`RecordedDirectionSource`); `internal/handler/tapcontext.go:388` — imzalı tap bağlamını `nfc`/`qr` dışında
  `errTapContextMalformed` ile **reddeder** (M16-07 bu bağlamı yeniden kullanırsa kayıt kaybı, §4.6);
  `internal/handler/claimqueue.go:615` (`claimChannelWord`); `internal/handler/tap.go:645` (önizleme kanalı);
  `internal/sandbox/simulate.go:118` (API kanal alanı), `:353` (`InFeed`); `internal/policy/guardrails.go:396`,
  `:479`, `:586`. (ters biçim `qr_live`'ı **sessizce kabul eder** — bu
  doğru olabilir, ama her biri için nedeni yazılır: ör. `db/queries/daycutoff.sql:73`, `00068:336`,
  `internal/domain/checkin/checkin.go:1777` (`SunValid` sütunu), `:1796` (site yazımı — `tagRow` varsayar, `qr_live`'da
  plaket satırı yoktur), ve `:1154` aşağıda) (başlangıç: ADR 0044 Bedel 6'nın listesi) — her biri için *"qr_live girer /
  girmez, neden"*. `transactions_site_by_channel` (`00043:139-142`) için üçüncü kol (ekranın lokasyonu dolu, plaket
  uid'i boş — öneri). Envanter en az şunları adlandırır: `00005`, `00043`, `00044`, `00046`, `00048`, `00049`, `00050`,
  `00067`, `00068`, `00095`; `db/queries/transactions.sql` — **`SecondsSinceLastRecordedTap`'in `channel IN ('nfc',
  'qr')` filtresi (`:317`) dahil**: debounce'un sunucu saati bacağıdır (ADR 0006); `qr_live` oraya girmezse geriye
  tarihlenmiş bir `qr_live` dokunuşu debounce'u sessizce atlar — ve `daycutoff.sql`.
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

- **Bağımlılık:** M16-20
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
    - **holding** (ADR 0027 kuralı açıksa): `site_tenant_id` = ekranın tenant'ı, `site_location_id` dolu, `site_tag_uid`
      NULL, `location_id` / `tag_uid` NULL — `transactions_site_by_channel`'ın (`00043:139-142`) M16-19'da eklenen
      `qr_live` kolu ve M16-19'un üçüncü çözümleyicisi.
  - **Token tenant'ı ↔ oturum tenant'ı:** başka tenant'ın canlı QR'ı → ret (`sys:tenant-mismatch`'in eşi); holding
    durumu (ADR 0027 — KM çalışanı KF ekranında) statik QR'ın holding kuralına (`sys:cross-tenant-qr-site-ip`) göre
    ya açılır ya kapalı kalır, gerekçesiyle; site sütunları (`site_tag_uid` NULL, `site_location_id` dolu) M16-19'un
    üçüncü koluyla uyumlu.
  - **`/v1` `tagId`:** `qr_live` olayında `null` (şema zaten `["string", "null"]`, `openapi.yaml:1130-1133`; açıklaması
    *"also for a QR punch"* der) — anlamı açıklamaya yazılır (Q38).
- `TestDecide_` önekli vakalar: `qr_live` + IP → `ok` · `qr_live` + GPS-only → `ok` (`base:gps-only-allow`) ·
  `qr_live` + hiçbiri → `flag` · aynı kişi 60 sn içinde → `ignored` · **kanallar arası:** `nfc` dokunuşundan 60 sn
  içinde `qr_live` → `ignored` (ve tersi) · **geriye tarihlenmiş** `occurred_at`'li `qr_live` öncülü 60 sn içinde
  yazılmışsa → `ignored` (sunucu saati bacağı, `SecondsSinceLastRecordedTap` — DB testi) · farklı iki kişi aynı
  token'la art arda → ikisi de kayıt · statik QR'ın sonuçları **aynen**.
- **Log testi:** token, imza ve doğrulama hatasının metni log'a ve hata gövdesine girmez (CLAUDE.md §7).
- Yeni baseline belgesi gerekip gerekmediği bu vakalarla ölçülür (ADR 0044 §2.5: büyük olasılıkla gerekmez); gerekirse
  `BaselineVersion` + duyuru.
- Rapor, liste, anomali ekranı ve `/v1` `qr_live`'ı tap kanalı olarak sayar (M16-19 envanterinin Go yarısı).
- **Eksiksizliğin kanıtı — parite matrisi (mekanizma, desen değil).** Gerçek yoldan uçtan uca DB testleri: her
  hücrede bir `qr` ve bir `qr_live` dokunuşu imzalı tap bağlamı → `checkin` → satır yolundan geçirilir.
  - **Amaç:** `qr_live`, aynı kanıtla `qr`'nin yazdığı her şeyi yazar — `transactions`, `punch_events` ve (holding
    kuralı `qr_live`'a açıksa) holding'de `site_punches`; bir şeyi eksik yazarsa test kırmızıdır ve envanterde adı geçmeyen bir yer de böyle yakalanır.
  - **Yalıtım:** iki kanal aynı çalışanla art arda koşmaz (ikinci dokunuş debounce'a düşer ya da yönü çevirir);
    **varsayılan, birebir aynı fikstürde klonlanmış çalışandır**. Gerekçe: gerçek yol commit eder; dış transaction
    yolu (`checkin.Database` tüketici tarafı arayüzdür, `checkin.go:141-144`) **ayrı bir test bağdaştırıcısı** ister —
    sandbox'ınki (`onRunner`, `internal/sandbox/runner.go:96-107`) dışa açık değildir ve `errAnotherBusiness` bir sandbox
    kuralıdır, mekanizmanın sınırı değil.
  - **Matris {yerel, holding} × {`ok` dokunuş, `flag` → müdür onayı}** — akış tetikleyicilerinin `qr_live`'ı bir
    `NOT IN` / `IN ('nfc', 'qr')` süzgeciyle sessizce atlayan dört kolu: `00067:193` (yerel `ok`), `:234` (yerel
    onay), `:366` (holding `ok`), `:408` (holding onayı). Holding hücreleri ADR 0027 kuralı `qr_live`'a **açıksa**
    parite ölçer. **Kapalıysa parite değil şu ölçülür** — holding kuralı statik `qr`'yi hiç reddetmez (K-1g,
    `sys:cross-tenant-qr-site-ip`: IP ile `ok`, IP'siz `flag`), bu yüzden bu dalda `qr` hücresi referans değildir:
    `qr_live` reddi bir `transactions` satırı yazar (§4.6) · `punch_events`'e olay girmez · `site_punches` satırı
    yazılıyorsa `outcome='refused'` ile **CHECK hatasız** yazılır (kapalı küme `00046:139-140`; bugünkü eşleme
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

---

# Faz F — Yayın + pilot

## M16-27 — Gizlilik: `/legal/privacy`, mağaza etiketleri

- **Bağımlılık:** M16-26 · Q42 · kullanıcı/hukukçu onayı
- **Araç:** yapıcı · **Mercek: tek**
- **Commit:** `docs(legal): describe the staff app`

**Amaç.** Uygulamanın topladığı ve toplamadığı veriyi hukuk metnine ve iki mağazanın gizlilik beyanına doğru yazmak.

**Kabul kriterleri.**
- `docs/legal/` taslağına uygulama paragrafı: toplanan veri (cihaz açık anahtarı, kaba cihaz etiketi, tap anında konum,
  sahte konum bayrağı, attestation sonucu), **toplanmayan** (biyometri, arka plan konum, reklam kimliği, analitik).
  Yayın operatör yüzeyinden (`/operator/legal`) kullanıcı tarafından.
- App Store gizlilik etiketi ve Play *Data safety* formunun cevap taslağı, her satır bir kod yerine bağlı.
- Pazarlama cümleleri Q42'nin cevabına göre (`web/templates/pages/*view.go` olgu sistemi, `FactTapPageNeedsNoApp`).

## M16-28 — İmza, iç test dağıtımı, sürüm süreci

- **Bağımlılık:** M16-27 · kullanıcı: mağaza hesapları, imza anahtarı saklama yeri
- **Kırmızı çizgi:** §4.7 (imza anahtarları repoda yok)
- **Araç:** yapıcı + kullanıcı · **Mercek: iki**
- **Commit:** `docs(mobile): document signing and internal releases`

**Amaç.** İmza anahtarlarını repo dışında tutarak iki mağazada iç test dağıtımını ve geri alınabilir bir sürüm sürecini
kurmak.

**Kabul kriterleri.**
- Android: Play App Signing; yükleme anahtarı repoda değil (Infisical); `assetlinks.json` parmak izi Play App
  Signing sertifikasınınki (M16-08 env'i güncellenir).
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
- ADR 0044 *"Uygulandı"*; A3, P6, Q11 satırlarının durumu ölçüyle güncellenir; ADR 0005 risk tablosu son hâli.
- **ADR 0005'e ek not** (append kuralı, satır silinmez): risk 1'in *"tek çözümü biyometri veya uygulama kurulumu —
  ikisi de ürünün varlık sebebine aykırı"* gerekçesi (`docs/adr/0005-kabul-edilen-riskler.md:74`) ve risk 2'nin
  *"attestation uygulama ister (app-less vaadi)"* gerekçesi (`:75`) isteğe bağlı uygulamayla yeniden okunur: uygulama
  var ama buddy punching'i çözmez (cihaz anahtarı devretmeyi durdurmaz), attestation yalnız kanıttır (ADR 0044 §2.7).
  `TestADR0005_TheAnchorCountsMatchTheProse` ve `TestADR0005_TheRiskCountMatchesTheTable` yeşil.
- `state.md`: M16 bitti.

---

## Sonra (numarasız — kart açılınca sıradaki numarayı alır)

- **Çevrimdışı kuyruk** (M9-01 ile birlikte): kuyruktaki dokunuş sunucu saatini alır (ADR 0038), `base:queued-window`
  eşiği ötesi `flag`; imzalı istek + nonce kuyrukta nasıl yaşar ayrı karar (nonce süresi dolar). Ayrı ADR.
- **Unutulan çıkış bildirimi — konumsuz:** saat ve açık giriş üzerinden yerel/push bildirim; §4.2 gereği konum
  tetikleyicisi yok.
- **"Saatlerim" ekranı** (M9-05'in uygulama yarısı).
- Attestation'ı karar verici yapmak — ayrı ADR.
