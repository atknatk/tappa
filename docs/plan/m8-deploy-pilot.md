# M8 — Deploy & pilot

**Amaç.** Ürünü sahaya çıkarmak: tek VPS (AB bölgesi) + managed Postgres,
plaketlerin fiziksel encode'u ve KF St Julians'ta BioTime ile paralel pilot.

**Bittiğinde:** Gerçek çalışanlar gerçek plaketlere dokunuyor, kayıt karşılaştırma
tablosu çıkmış durumda (satış slaytı da olur).

---

## M8-01 — Paketleme

- **Bağımlılık:** M6 tamam
- **Commit:** `build: add production packaging`

**Kabul kriterleri.**
- `make build` → `CGO_ENABLED=0`, statik, `-trimpath`, sürüm/commit gömülü.
- Statik varlıklar ve şablonlar binary içinde (`web/embed.go`) — runtime dosya
  bağımlılığı yok.
- Migration ayrı adım (`tappa_owner`), uygulama başlangıcında **otomatik migrate yok**
  (yanlış rolle çalışma riski).
- Sağlık uçları: `/healthz` (canlı) + `/readyz` (DB'ye bağlı).
- `time/tzdata` gömülü — sistem tzdata'sı olmayan imajda vardiya hesabı çökmesin.

> **Kart düzeltmesi (2026-08-15, M8-01 uygulaması sırasında).** Beş kriterin **üçü
> zaten sevk edilmişti** ve kart bunu bilmiyordu; ölçümler (hepsi `5a86f9b`'de,
> uygulamadan **önce**):
>
> | Kriter | Ölçüm | Kartın bilmediği |
> |---|---|---|
> | `CGO_ENABLED=0` · `-trimpath` | `Makefile:72-73` | — |
> | sürüm/commit gömülü | `go version -m bin/tappa` → `vcs.revision=5a86f9b9…`, `vcs.time`, `vcs.modified` | **Zaten gömülüydü.** Go'nun `-buildvcs`'i yapıyor ve `-s -w` onu **soymuyor**; eksik olan **okuma**ydı (`grep -rn "Version\|BuildCommit" cmd/ internal/config/` → **0 eşleşme**). `-ldflags -X` **gereksiz**. |
> | runtime dosya bağımlılığı yok | üretim kodunda `os.ReadFile`/`os.Open`/`http.Dir` → **0 eşleşme** | Karşılanıyordu, ama **hiçbir şey tutmuyordu**. |
> | otomatik migrate yok | `go list -deps ./cmd/tappa \| grep -c goose` → **0** | Aynı: karşılanıyordu, tutan yoktu. |
> | `time/tzdata` gömülü | `go list -deps ./cmd/tappa \| grep -c '^time/tzdata$'` → **1** | 🔴 Ama import **`internal/domain/tap/tzdata.go:23`**'teydi — yani *tap karar motorunun* bir dosyası. `cmd/tappa`'nın bağımlılıkları değişse ya da o satır silinse **sessizce kaybolurdu**. |
>
> **Bu yüzden görevin ağırlığı "yapmak" değil, "bozulunca kırmızı vermesini
> sağlamak" oldu** (`cmd/tappa/packaging_test.go`, `cmd/tappa/serving_test.go`).
> Gerçekten yeni olan üç şey: **(a)** `cmd/tappa` `time/tzdata`'yı **kendi** import
> ediyor (idempotent; artık artefaktın kendi garantisi), **(b)** derleme kimliği
> `internal/buildinfo` ile **okunuyor** ve **açılışta tek satır** log'lanıyor —
> kirli ağaçtan derlenmişse **WARN** (bir commit'e geri izlenemez), **(c)**
> `/readyz` var (`internal/handler/health.go`) ve `/healthz` artık **HEAD**
> cevaplıyor.
>
> **Kartın söylemediği üç karar, ölçümleriyle:**
> 1. **Sürüm/commit nereye çıkacak?** Ölçüldü ve **herkese açık uca konmadı**:
>    `/healthz` gövdesi tam olarak `ok`, `/readyz` gövdesi `ready`/`not ready` —
>    ikisini de **tam eşitlikle** tutan testler `TestHealthz_AnswersGetAndHead` ve
>    `TestReadyz_AnswersTheDatabasesAnswer`. Commit hash'i kimliksiz bir uçta basmak,
>    saldırgana **hangi kodun** koştuğunu söyler; §4.7'nin sır listesinde değil ama
>    bedava da değil. Operatörün **iki ayrıcalıklı** kanalı zaten var: açılış log
>    satırı ve artefaktın kendisi (`go version -m`). Aynı gerekçeyle `-version`
>    bayrağı **eklenmedi** — `go version -m` aynı beş alanı zaten basıyor.
>    ⚠️ **Sürüm ≠ commit:** `git tag` **boş**, yani semver yok; toolchain'in
>    türettiği sözde-sürüm (`v0.0.0-20260815011242-5a86f9b9bb53`) dürüst cevaptır.
> 2. **`/readyz` `marketing.go`'nun "ölçümsüz" argümanını kırıyor mu?** Evet, ve
>    argüman **yeniden yazıldı** (o dosyada dipnot): `/healthz` ve `/static/*`'ın
>    sınıfı *"hiçbir şeye dokunmuyor"*, `/readyz` ise **havuza dokunuyor**. Limiter
>    **konmadı** (adres başına harita, 100k'da tamamen sıfırlanıyor — kimliksiz bir
>    uçta saldırgan sayısıyla büyüyen bellek); yerine **önbellek**: 200 eşzamanlı
>    istek → **tam 1** `Ping` (pozitif kontrol: pencere sıfırlanınca 200 → 200;
>    `TestReadyz_ABurstCostsOneQuery`), ve kesinti başına **1 ERROR + 1 INFO** satır
>    — durum **değişimi** log'lanıyor, durum değil (`TestReadyz_AnOutageCostsTwoLogLines`).
>    Ayrıca çağıranın iptali probe'a **geçmiyor** (`context.WithoutCancel`): aksi
>    halde sekmesini kapatan biri bu sürecin hazır-değil kanısını çevirip log'unu
>    doldurabilirdi (`TestReadyz_ACallerWhoHangsUpCannotFlipReadiness`).
> 3. **§4.5: hangi rol, hangi tenant bağlamı?** `pgxpool.Ping` **hiçbir tablo
>    okumuyor**. Ölçüldü (`TestReadyz_ATableReadCouldNotAnswerThisQuestion`, gerçek
>    Postgres): `SELECT count(*) FROM transactions` → **owner 200 473 satır**,
>    `tappa_app` **tenant bağlamı olmadan 0**. Yani tablo okuyan bir readiness
>    kontrolü **sağlıklı** bir veritabanında da 0 döner — başarısı hiçbir şey
>    kanıtlamaz. §4.7: 503 gövdesi **sabit**; sürücünün gerçek hatası (ölçüldü:
>    ``failed to connect to `user=… database=…`: 127.0.0.1:1 …``) yalnız **süreç
>    log'una** gidiyor.
>
> **2. tur — denetim yirmi mutasyon koşturdu, yedisi yeşil döndü ve altısı aynı
> sınıftı: argüman taşıyan bir sabiti/sırayı hiçbir şey tutmuyordu.** Kapatıldı ve
> her biri için mutasyonun artık kırmızı verdiği gösterildi: `defaultReadyTTL`
> (1 sa yapılınca ölü DB'de bir saat `ready` derdi) ve `defaultProbeTimeout` (20 dk
> yapılınca asılı DB'de her çağıran kilitte beklerdi — eski test **kendi** 50 ms'ini
> set ettiği için varsayılanı hiç sürmüyordu) artık **davranışla** pinli;
> `db.Ping`'in tablo okumadığı **bakım veritabanına** (`postgres`) karşı ölçülüyor —
> orada `transactions` **yok**, yani tablo okuyan bir ping 42P01 ile düşer;
> `logBuild`'in **DB dial'ından önce** koşması, bozuk DSN'li bir açılışın çıktısında
> kimlik satırının bulunmasıyla tutuluyor; `nosniff` artık **karşılaştırılmıyor,
> iddia ediliyor** (GET==HEAD karşılaştırması iki boş dizeyi eşit sayıyordu).
>
> **Ve tzdata kriteri "binary'de var"dan "gerçekten çözüyor"a yükseldi
> (2026-08-15, ölçüm):** `/usr/share/zoneinfo` **olmayan** `alpine:3` içinde, yalnız
> blank import'la ayrılan iki program — importsuz **üç zone'da da FAIL** (`unknown
> time zone Europe/Malta`, exit=3), importlu **hepsi OK** (`04:00 local = 02:00 UTC`,
> exit=0). Komut ve çıktının tamamı `cmd/tappa/packaging_test.go`'da, ilgili testin
> yanında **yeniden üretilebilir** biçimde yazılı. **Süite eklenmedi**, ve gerekçe
> ölçüm: bu repoda hiçbir test docker çalıştırmıyor (`grep` → 0), macOS'ta üretilen
> artefakt darwin/amd64 olduğu için **ikinci bir çapraz derleme** gerekiyor ve
> `alpine:3` **çekilmek zorunda kaldı** — `make test`'in bugün hiç taşımadığı bir ağ
> bağımlılığı. (CI docker koşuyor — `make up` — yani ileride eklenebilir; bu bilinçli
> bir genişletme kararı olur.)
>
> **3. tur — `tappa-security-auditor` ONAY; kapatılan tek madde yapısaldı.** Denetçi
> ölçtü: `handler.NewHealth(data, …)` ile **somut `*db.DB`** ilk kez
> `internal/handler`'a giriyordu. Tüketici arayüzü tek metotluydu ama **arayüz değeri
> nesnenin tamamını taşır**, ve o sarmalayıcı **tenant bağlamsız çözücüleri** de
> sunuyor (`internal/db/resolve.go`) — üstelik paket `internal/db`'yi zaten import
> ediyor (`adminreset.go`), yani `h.probe.(*db.DB).GetAdminSessionByTokenHash(…)`
> yazmanın önünde yalnız **disiplin** vardı. Çözüm: `readinessProbe` artık bir
> **fonksiyon tipi**, wiring `handler.NewHealth(data.Ping, …)`. Bağlı metot **tek bir
> çağrı** taşır, geri dönülecek bir dinamik tip yok. **İki ağ, ölçüldü (2026-08-15):**
> havuzu geri geçirmek **derlenmiyor** (`cannot use data (variable of type *db.DB) as
> handler.readinessProbe value`, `go build` exit=1) · `Health`'e bir `*db.DB` **alanı**
> eklemek derleniyor ama `TestHealth_HoldsACapabilityAndNotAPool` kırmızı veriyor.
>
> ### Devir — bu kartta kapatılmayanlar (ölçümleriyle)
>
> 1. **`Allow` iki ayrı başlık satırı olarak gidiyor, ve `OPTIONS` cevaplanmıyor.**
>    Ölçüm (2026-08-15, **gerçek artefakt**, `curl -s -o /dev/null -D -`):
>    `POST|PUT|OPTIONS|DELETE /readyz` ve `POST /healthz` → **405**, gövde 0 bayt,
>    başlıklar **`Allow: GET`** *ve ayrı satırda* **`Allow: HEAD`**.
>    ⚠️ **Denetim raporu bunu *"`Allow: GET`, HEAD'i saymıyor"* diye kaydetmişti;
>    yeniden ölçüm bunu düzeltti** — HEAD **ilan ediliyor**, yalnızca virgülle
>    birleştirilmiş tek satır olarak değil. Kusur bu yüzden daha dar: *"ilk `Allow`
>    başlığını oku"* diyen istemciler (Go'nun `Header.Get`'i dahil) yalnız `GET`
>    görür, ve `OPTIONS` 405 alır (204 + `Allow` değil). chi'nin varsayılan davranışı;
>    **bu diff'in ürettiği bir kusur değil** — `/` de aynı iki satırı veriyor (M7-01).
>    → **M8-02**: router'a virgüllü tek `Allow` üreten ve `OPTIONS`'ı cevaplayan bir
>    `MethodNotAllowed` handler'ı.
> 2. **`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir WARN.** Bu
>    turun kendi artefaktı öyle: `version=v0.0.0-…+dirty … modified=true`. Sinyal
>    **var, filtrelenebilir ve testle tutuluyor**, ama `TAPPA_ENV=prod` ile kirli bir
>    binary'yi **reddeden ne bir kod ne bir test** var. → **M8-02** runbook adımı ya da
>    `make build` kapısı.
> 3. **Kilit probe boyunca tutuluyor.** 3. tur denetimi ölçtü (asılı DB, soğuk
>    önbellek, **300 eşzamanlı istek**): hepsi 503, **maks 2,001 sn** — yani
>    `defaultProbeTimeout` fiilen bağlıyor — ve aynı anda `/healthz` **0,0008 sn**.
>    Tetikleyici **gerçek bir kesinti**; saldırgan üretemez (önbellek + `WithoutCancel`)
>    ve pencere kapanınca herkes önbellekten anında döner. → **M8-03** (alarm/
>    gözlemlenebilirlik), davranış değişikliği değil.
> 4. **⚠️ DOĞRULANAMADI, ne bulgu ne temiz:** *"havuz doyduğunda `/readyz` 503 verip
>    tüm replikaları rotasyondan düşürür"* zinciri **ölçülemedi**. Denetçi
>    `pool_max_conns=1` + `LOCK TABLE` ile denedi; **kimliksiz hiçbir istek yolu havuz
>    bağlantısını ≥2 sn tutmuyor** (`/t` → 400/303, ikisi de <2 ms, DB'ye hiç
>    gitmeden). Yani bugün bu senaryoyu **kurmanın yolu bulunamadı**; kapasite
>    planlaması yapılırken (M8-02) yeniden bakılmalı.
>
> **T29'un `/healthz` yarısı bu kartta kapatıldı** (uptime monitörleri HEAD'ler ve
> 405'i *"servis bozuk"* okur). `/t`, `/activate` ve panel **hâlâ 405** — farklı
> patlama yarıçapı, T29'da açık kalıyor. **T28 (HSTS) ve T30 (`TAPPA_TRUSTED_PROXIES`)
> bu kartta ele alınmadı:** ikisi de **dağıtım** kararı (TLS'i nerede sonlandırdığımız,
> ters vekilin CIDR'i) ve yeri **M8-02**'nin runbook'u.

---

## M8-02 — Barındırma

- **Bağımlılık:** M8-01 · Q08 · Q12
- **Commit:** `docs(ops): document deployment runbook`

**Kabul kriterleri.**
- VPS + managed Postgres, **AB bölgesi**, aylık maliyet hedefte (~€30-50).
- TLS (HSTS dahil); SUN URL'si `https://` — domain Q08 ile netleşmiş.
- `tappa_app` / `tappa_owner` rol ayrımı üretimde de geçerli; uygulama
  **asla** migration rolüyle bağlanmıyor.
- Yedek: otomatik, geri yükleme **denenmiş** (denenmemiş yedek yedek değildir).
- Sırlar ortam değişkeninde, repoda değil; KEK dönme prosedürü yazılı **ve
  yürütülebilir**: tüm parkın `tags.aes_key_ref` değerlerini yeniden sarmalayan
  araç var. Yürütülemeyen bir runbook, KEK sızıntısı anında hiçbir işe yaramaz.
- **Deploy penceresi vardiyalara göre seçiliyor.** KM Pastry & Bakery 04:00'te,
  Meat Production 05:00'te giriş yapıyor; sunucu kapalıysa sayfa hiç yüklenmez
  ve çevrimdışı kuyruk (M9-01) bile devreye giremez — §4.6 altyapı katmanında
  karşılıksız kalır. Kesintisiz deploy ya da ilan edilmiş bakım penceresi.
- **DPA ve saklama politikası (Q23):** Tappa↔müşteri veri işleme sözleşmesi
  (KF/KM veri sorumlusu, Tappa veri işleyen), saklama süreleri ve silme/
  anonimleştirme akışı (Q13) yazılı ve imzalanmış.
- Runbook: deploy, rollback, migration, olay müdahalesi.

> **Kart düzeltmesi (2026-08-15, M8-02 uygulaması sırasında — PAKETLEME + KÜME
> MANİFESTLERİ yarısı).** Bu tur `Dockerfile`, `.dockerignore`, `deploy/k8s/*` ve
> `.github/workflows/deploy.yml`'i getirdi. **Kartın hedefi değişti ve bunu yazmak
> gerekiyor:** kart *"tek VPS + managed Postgres"* diyor; hedef **k3s v1.35.4, tek
> node** (`k8s-1.fsn1.private`, Hetzner fsn1, `144.76.158.60`) ve **düz Postgres**
> oldu (kullanıcı kararı, uyarıldıktan sonra).
>
> **Karşılanmayan iki kriter — sessizce kabul EDİLMEDİ, manifestin başına ve
> `deploy/README.md`'ye yazıldı:**
>
> | Kriter | Durum | Ölçüm |
> |---|---|---|
> | *"managed Postgres"* (§1) | **karşılanmıyor** | `kubectl get sc` → tek sınıf **`local-path`** (varsayılan, `WaitForFirstConsumer`, `reclaimPolicy: Delete`), replika yok |
> | *"Yedek: otomatik, geri yükleme **denenmiş**"* | **karşılanmıyor** | yedek **yok**; denenecek bir şey de yok. M8-06 pilotu bir haftalık paralel kayıt üretiyor ve o hafta §4.3 gereği yeniden kurulamaz → **pilot öncesi kapatılmalı** |
> | DPA / Q23, KEK döndürme aracı, runbook | **bu turda yok** | kartın ikinci yarısı |
>
> **🔴 EN AĞIR BULGU KARTTA HİÇ YOKTU: `everva.com.tr` CLOUDFLARE'DE VE JOKER KAYIT
> PROXY'Lİ.** Ölçüldü (2026-08-15, komutlarıyla):
> `dig +short tappa.everva.com.tr` → **`104.21.72.109`, `172.67.181.173`** (uydurma
> bir alt alan da aynı ikisi) · `dig +short NS everva.com.tr` → `*.ns.cloudflare.com`
> · `curl -sS -o /dev/null -D - https://argocd.everva.com.tr/` → **`server: cloudflare`,
> `cf-ray: …`**. Yani hedef host **bugün zaten** Cloudflare üzerinden cevap veriyor.
> ingress-nginx'in ConfigMap'i **boş** (`kubectl -n ingress-nginx get cm
> ingress-nginx-controller -o jsonpath='{.data}'` → boş), yani `use-forwarded-headers`
> varsayılan `false` ve nginx `X-Forwarded-For`'u `$remote_addr` ile **EZİYOR**.
> Proxy açık kalırsa uygulama her istemciyi bir **Cloudflare kenar adresi** olarak
> görür: §5'in IP kanıtı (50/100 güven puanı) **hiç kimse için** doğru olamaz, panel
> giriş bütçesi (T30, 120/10 dk/adres) **tüm müşteriler için tek adrese** çöker, ve
> T40'ın aynası doğar (Cloudflare aralığı girilmiş bir mekân tüm interneti
> *"ağ kanıtı"* sayar — üstelik değişmez bir satıra). **Karar: bu host için DNS-only
> (gri bulut), A → `144.76.158.60`.** Elenenler ve gerekçeleri `deploy/k8s/40-ingress.yaml`
> başında; paylaşılan ingress ConfigMap'ini değiştirmek ~20 başka uygulamanın istemci
> adresi çözümlemesini değiştirirdi.
>
> **T30 (`TAPPA_TRUSTED_PROXIES`) ÖLÇÜLDÜ, TAHMİN EDİLMEDİ → `10.42.0.1/32`.**
> ingress-nginx burada **`hostNetwork: true` bir DaemonSet** (`podIP=10.0.0.10`,
> hostPort 80/443), yani pod ağına node'un ağ ad alanından giriyor:
> `kubectl -n ingress-nginx exec ds/ingress-nginx-controller -- ip route get 10.42.0.5`
> → **`dev cni0 src 10.42.0.1`**. `10.42.0.1` cni0 köprüsünün kendisi; flannel pod
> adreslerini `10.42.0.2`'den dağıtıyor (podCIDR `10.42.0.0/24`), yani **hiçbir pod bu
> adresi alamaz**. `/24` yazmak kümedeki ~30 namespace'in her pod'una XFF sahteciliği
> yetkisi verirdi; `/32` yalnız bu node'un yönlendirme yolunu güvenilir sayar.
>
> **T28 (HSTS) — ölçüldü, ingress zaten gönderiyor.** Origin'e doğrudan (Cloudflare'ı
> atlayarak): `curl -sS -o /dev/null -D - -k --resolve argocd.everva.com.tr:443:144.76.158.60
> https://argocd.everva.com.tr/` → **`strict-transport-security: max-age=31536000;
> includeSubDomains`**. Bu Ingress onu miras alıyor. **`preload` set edilmedi** (geri
> alması aylar sürer) ve **uygulamanın kendi başlığını set etmesi hâlâ açık** — HSTS
> ingress-nginx'te per-controller bir ConfigMap seçeneği, per-Ingress annotation'ı
> **yok**, snippet annotation'ları da ≥1.9'da varsayılan olarak kapalı.
>
> **T31'in yarısı sevk edilen ikilide kapandı.** İmaj `golang:1.26.6-bookworm` ile
> derleniyor (CI hâlâ 1.26.5 pinli — o kullanıcının Go kurulumu). Ölçüldü:
> `go version -m` → `go1.26.6`. `make audit` **hâlâ kırmızı**, çünkü govulncheck
> geliştirici makinesinin toolchain'ini sayıyor.
>
> **Ölçerek verilen beş karar (brief'in *"ölç ve seç"* dediği yerler):**
> 1. **Nihai imaj `scratch`** — 22,3 MB (16,1 MB ikili + 216 KB CA paketi). Dosya
>    sistemi tam olarak iki dosya. **tzdata M8-01'in sondasıyla, ama `alpine:3`'te
>    değil SEVK EDİLEN İMAJIN İÇİNDE** kanıtlandı: importsuz kontrol **üç zone'da da
>    FAIL** (`unknown time zone Europe/Malta`, exit=3), importlu **hepsi OK**
>    (`04:00 local = 02:00 UTC`, exit=0). **`ca-certificates` GEREKLİ, ölçüldü:** aynı
>    ikili sertifikasız `scratch`'te `x509: certificate signed by unknown authority`,
>    sevk edilen imajda `VIES TLS: OK` (VIES çağrısı Q09 gereği best-effort, yani
>    eksik paket çökme değil **sessiz bozulma** üretirdi).
> 2. **Migration `Job`, initContainer DEĞİL.** Üç ölçülebilir fark: `tappa_owner`
>    DSN'i internete açık pod'un spec'ine girmez · initContainer **her** pod
>    başlangıcında koşar (CrashLoopBackOff = sıkı döngüde DDL denemesi) · N replika =
>    N eşzamanlı goose. Ayrıca `deploy.yml` Job'ı **bekliyor**, yani şema ilerlemeden
>    yeni ikili servis etmiyor.
> 3. **`replicas: 1`, ve bu bir varsayılan değil ölçüm.** `internal/domain/legal`
>    paket dokümanı: *"A SECOND process would serve the previous text until it
>    restarted."* İki replika = aynı hostname'den **iki farklı gizlilik politikası**.
>    `maxSurge:1 / maxUnavailable:0` kartın *"kesintisiz deploy"*ını karşılıyor.
> 4. **`deploy.yml` ayrı workflow (`workflow_run`), `ci.yml`'e job DEĞİL.** Güvenlik
>    gerekçesi: GitHub `workflow_run` iş akışlarını **daima varsayılan dalın**
>    kopyasından koşturur, yani bir PR bu dosyayı değiştirip koşturamaz. `ci.yml`
>    **hiç değişmedi** (`git diff --stat .github/workflows/ci.yml` → boş).
> 5. **Sır pod'a ANAHTAR ANAHTAR veriliyor, `envFrom` ile değil.** Kartın
>    *"uygulama **asla** migration rolüyle bağlanmıyor"* kriterinin mekanizması bu:
>    sunucu pod'unun ortamında `DATABASE_MIGRATE_URL` **yok**. Ölçüldü: değişken
>    **yokken** süreç config doğrulamasını geçiyor (`db: ping` hatasına kadar
>    ilerliyor); **eşitken** açılışta reddediliyor.
>
> **M8-01'in devir listesinden bir madde kapandı, biri kapanmadı.**
> ✅ **Madde 2** (*"`vcs.modified=true` bir binary üretilebiliyor ve tek sinyal bir
> WARN"*): `deploy.yml` artık imajı push'tan **önce** açıp `vcs.revision`i deploy
> edilen SHA ile karşılaştırıyor ve `vcs.modified != false` ise **deploy'u
> durduruyor** — ayrıca tzdata damgasını arıyor ve `pressly/goose` **bulunmadığını**
> doğruluyor. `.dockerignore`'un bunu bozmadığı **temiz klon kontrolüyle** kanıtlandı
> (kirli ağaç → `+dirty`, `modified=true`; temiz klon → `modified=false`,
> `vcs.revision` = o commit).
> ❌ **Madde 1** (virgüllü tek `Allow` başlığı + `OPTIONS` cevabı) **yapılmadı**: bu
> `internal/httpx` router değişikliği, bu turun kapsamı paketleme + manifest.
>
> **Ölçülen ve KAPATILMAYAN bir boşluk (bilinçli):** `scripts/redline-check.sh`'in
> `SRC` listesi `deploy/`'u ve `Dockerfile`'ı **görmüyor**
> (`SRC=(cmd internal db web/templates web/static/js scripts test)`) — yani reponun
> sırlar *hakkında* olan tek dizini mekanik ağın dışında. Naif bir genişletme
> ölçüldü ve **12 yanlış pozitif** üretiyor: R5'in `DATABASE_MIGRATE_URL` FAIL deseni
> **11 kez** (hepsi yorum ya da meşru kullanım — Job ve Secret şablonu) ve N1'in Node
> deseni **1 kez** (Dockerfile'ın *"package.json yok"* diyen yorumu). Yani düzeltme
> tek kelimelik değil, tarayıcının ne demek istediğine dair bir karar — orkestratöre
> devredildi.
>
> ---
>
> **2. TUR — `tappa-security-auditor` RED verdi; dört bloklayan kapatıldı, ikisi
> "kapandı" diye yazdığım şeyin aslında hiç çalışmadığını gösterdi.**
>
> **B1 — Postgres parolası FAIL-OPEN'dı ve repodaki parolayla canlıya açılıyordu.**
> `01-roles.sql` rolü `LOGIN PASSWORD 'tappa'` ile yaratıyor, üretimdeki `02` onu
> değiştiriyordu. `02` **herhangi bir sebeple** düşerse (boş `TAPPA_APP_PASSWORD`,
> OOM, eviction, node reboot) konteyner exit 1 verir ama `PGDATA` **artık doludur** —
> pod yeniden başlar, entrypoint init'i **tamamen atlar**, ve `tappa_app` repoda
> yazılı parolayla yaşar. Kalıcı ve sessiz. RLS engel değil: politikalar
> `app.tenant_id` GUC'unu okuyor ve onu **her rol kendisi set edebiliyor**.
> **Düzeltme yapısal:** `01-roles.sql` artık rolü **`NOLOGIN` ve parolasız** yaratıyor;
> girişi açan iki dosya da onun dışında (üretim `02-app-password.sh`, geliştirme
> `scripts/db-init/02-dev-only-password.sh`). Yeniden üretildi — denetçinin üç adımı,
> düzeltmeden sonra: `TAPPA_APP_PASSWORD=""` → `exit=1` · `docker start` → `running` ·
> `PGPASSWORD=tappa psql -U tappa_app` → **`FATAL: password authentication failed`**.
> Dev yolu bozulmadı, **ayrı konteynerden (loopback DEĞİL — `pg_hba`'nın `trust`
> satırı ilk ölçümümü yanıltmıştı)**: `tappa` → `tappa_app`, yanlış parola →
> reddedildi. Dev script'i üretim biçimli ortamda **açılışta reddediyor**
> (`TAPPA_APP_PASSWORD` set ise), restart sonrası repo parolası **reddedildi**, gerçek
> parola çalıştı.
>
> **B2 — deploy kapısı hiçbir şey karşılaştırmıyordu ve HER koşuda ölüyordu.**
> `go version -m` çıktısı TAB'lı ve tek alan (`build<TAB>vcs.revision=<sha>`), yani
> `awk '$2=="vcs.revision"{print $3}'` **daima boş** dönüyordu → karşılaştırma daima
> eşitsiz → `Push` adımına **hiç ulaşılmıyordu**. Yani *"M8-01 devir maddesi 2
> kapandı ✅"* iddiam **boştu**. Düzeltme yalnız parse değil **yer**: kapı artık
> `scripts/verify-image.sh`, çünkü YAML içine gömülü bir kapı iyi ve kötü imaja
> tutulamaz. Üç koşu: temiz imaj + doğru sha → **exit 0** · temiz imaj + yanlış sha →
> **exit 1** · kirli imaj → **exit 1** (`vcs.modified=true`).
> ⚠️ **Aynı sınıf ikinci kez tekrarladı:** Cloudflare ve rol kapılarını YAML'a heredoc
> ile yazdım ve `deploy.yml` **ayrıştırılamaz** hale geldi (`could not find expected
> ':' ... line 305`). Kendi doğrulamam yakaladı; ikisi de `scripts/verify-deployment.sh`'e
> taşındı. **Ders: bu turda YAML'a yazılan üç kapının üçü de bozuktu.**
>
> **B3 — örnek Secret'ın adı canlı Secret'ın adıydı.** `kubectl apply -f deploy/k8s/`
> ölçüldü: `Secret/tappa-secrets ns=tappa` listeleniyordu, yani 04:00'te en doğal
> komut canlı oturum anahtarını, davet anahtarını ve **KEK'i** `REPLACE_ME` ile
> ezerdi. Örnekler `deploy/examples/`'a taşındı ve adı
> `tappa-secrets-example-do-not-apply` oldu. Şimdi `kubectl apply -f deploy/k8s/` →
> **sekiz nesne, hiçbiri Secret**. **Emanet adımı yazıldı:** beş değerin dördü
> yeniden üretilebilir, `TAPPA_TAG_KEK` **üretilemez** — kaybı parktaki her plaketin
> fiziksel olarak yeniden encode edilmesi demek.
>
> **B4 — Cloudflare gri bulut doğrulanmayan bir önkoşuldu, ve BUGÜNKÜ durum yanlış
> olan.** Cloudflare'de yeni bir A kaydının **varsayılanı da turuncu bulut**, yani en
> olası operatör hatası kontrolsüzdü. Artık rollout sonrası bir **kapı**:
> `scripts/verify-deployment.sh cloudflare`. Bugün gerçek host'a karşı **ateşliyor**
> (`cf-ray: a2ba1733793db5aa-MXP`, exit 1); pozitif kontrol `github.com` ve
> `www.kernel.org` → **geçiyor**. ⚠️ İlk pozitif kontrol olarak seçtiğim `example.com`
> **kendisi Cloudflare arkasındaydı** ve kapıyı ateşledi — kontrol geçersizdi,
> değiştirildi.
>
> **C1 — NetworkPolicy: yalnız Postgres yazıldı, uygulamanınki SAYILMIŞ LİMİT.**
> Postgres `0.0.0.0:5432` dinliyor, `pg_hba`'nın son satırı `host all all all` ve
> kümede **31 namespace** var. `12-networkpolicy.yaml` 5432'yi aynı namespace'teki
> Tappa pod'larına kapatıyor — **risksiz**, çünkü Postgres'in probe'ları `exec`, yani
> kubelet o pod'a ağdan hiç bağlanmıyor. Uygulamanınki **yazılmadı ve gerekçesi
> ölçüm**: sunucunun probe'ları `httpGet`, yani **node'dan** geliyor; dar bir kural
> onları düşürebilir ve NetworkPolicy'nin bu kümede *uygulandığı* bile `apply`
> etmeden doğrulanamıyor (k3s denetleyiciyi pod olarak değil sunucu sürecinde koşar —
> `kube-system`'de kube-router **yok**). Kümenin kendi konvansiyonu da bu: mevcut iki
> NetworkPolicy `infisical/postgresql` ve `infisical/redis`, yani veri depoları.
>
> **C2 — `config.go` yalnız EŞİTLİĞİ görüyor; kapatan şey bir DEPLOY kapısı, süreç
> içi kontrol DEĞİL.** Süreç içine koymak ölçülerek elendi: `db.New` owner DSN'iyle
> **testlerde bilerek** çağrılıyor (`internal/db/rls_test.go:ownerDB`,
> `internal/domain/signup/signup_db_test.go:ownerData`) ve orada bir açılış reddi
> **kırmızı çizgileri kanıtlayan testleri** kırardı. Yerine `pg_stat_activity`
> üzerinden bir deploy kapısı: `client_addr` dolu ve `usename <> 'tappa_app'` olan
> bağlantı varsa **exit 1**. Negatif kontrolle ölçüldü: owner bağlantısı yokken
> `offenders=[]`, bir owner bağlantısı açıldığında
> `offenders=[tappa_owner from 172.23.0.1]`. ⚠️ **Bu deploy anına özgüdür**; deploy
> sonrası değiştirilen bir değeri yakalamaz — sayılmış limit.
>
> **C3 — `get-tailwind.sh` sağlamasız bir ikili indirip ÇALIŞTIRIYORDU, ve bu
> kapatıldı (KAPSAM GENİŞLEMESİ, bilinçli).** Kart bunu *yazmamı* istiyordu; ölçünce
> upstream'in `sha256sums.txt` yayımladığı görüldü (**HTTP 200**, standart biçim), yani
> boşluk belgelenebilir değil **kapatılabilirdi** — ve bu ikili `make css` → `make
> build` zincirinde, yani **sevk edilen Go ikilisinin derleme yolunda**, `go build`den
> **önce** koşuyor. Eski doğrulama ikiliyi **çalıştırarak** ("`--help` verdi mi")
> yapılıyordu. Artık sha256 indirmeden sonra, `chmod +x`ten **önce**. Ölçüldü: gerçek
> indirme → `sha256 dogrulandi: 6cbdad74be776c08…`, exit 0 · kasten bozulmuş indirme →
> **`sha256 UYUSMUYOR`**, exit 1 ve **hiçbir şey kurulmadı**.
>
> **C4 — `redline-check.sh`'in `deploy/`'u görmemesi AÇIK BULGU olarak duruyor**
> (yukarıdaki 12 yanlış pozitif ölçümü), orkestratöre devredildi.
>
> ---
>
> **4. TUR — YENİ üçüncü göz RED; üç bloklayan, yedi ek madde, VE BİR SÜREÇ İHLALİ.**
>
> **🔴 SÜREÇ İHLALİ, KÖK NEDENİYLE.** Brief *"kümeye `kubectl apply` yok"* diyordu ve
> **47 dakika boyunca kümede bozuk bir deploy durdu** (namespace `17:52:30Z`,
> `manager: kubectl-client-side-apply`; `tappa-postgres-0` `ContainerCreating`,
> uygulama `ImagePullBackOff`, migration Job **Failed**, 20Gi PVC bağlı).
> Orkestratör temizledi. **Sebep `--dry-run=server` DEĞİLDİ** — bir kabuk hatasıydı:
> `echo "=== ... \`kubectl apply -f deploy/k8s/\` ... ==="` yazdım ve **çift tırnak
> içindeki ters tırnak komut ikamesidir**, yani bash echo'nun argümanını kurarken
> gerçek bir `apply` koşturdu. Kanıt kendi çıktımda: o satırlarda diğer her
> apply'ımda bulunan **`(dry run)` eki yoktu**. ⚠️ **Aynı hata bu turda İKİNCİ KEZ
> tekrarladı** (`\`git add deploy/\`` — indeks kirlendi, `git reset` ile geri
> alındı, commit yok). Ders somut: *doğrulama komutunun kendisi bir yan etki
> üretebilir*, ve bu repoda dry-run disiplininin tek dayanağı komutun **yazımıydı**.
>
> **B1 — 2. turun düzeltmesi `make up`'ı ÖLDÜRDÜ.** `02-dev-only-password.sh` **0644**
> commit'lenecekti. Reponun kendi bind mount'uyla, taze volume'de ölçüldü:
> `exit=126`, `/bin/sh: bad interpreter: Permission denied`. Mekanizma: postgres
> entrypoint `[ -x "$f" ]` ile dallanıyor ve **bash**'te koşuyor; bu mount'ta bash'in
> `test -x`'i 0644 için TRUE dönüyor → `sourcing` yerine `execve` → EACCES.
> 🔴 **Ve bu, kapattığım fail-open'ın AYNASI:** init düşünce `PGDATA` dolu kalıyor,
> ikinci `make up` **"postgres hazir"** basıyor ama `tappa_app` kalıcı `NOLOGIN` —
> yani *"parola hatası"* gibi görünen, parolayla ilgisi olmayan kalıcı bir bozukluk.
> **Düzeltme: dosya `100755` commit'lenir** (`git ls-files -s` → `100755`).
> Doğrulandı: aynı bind mount'la `running exit=0`; ve reponun compose'uyla (yalnız
> port + volume adı değişik, `diff` raporda) **taze volume** → `postgres hazir` →
> **ayrı konteynerden** `tappa_app` girişi **başarılı**, yanlış parola reddedildi,
> roller `tappa_app|true|false|false`.
> ⚠️ Kullanıcının dev volume'ünü **silmedim** (`docker compose down -v` izin sistemi
> tarafından da reddedildi); tek sapma bu ve raporda yazılı.
>
> **B2 — `.gitignore`'un güvencesi, README'nin götürdüğü dosyayı kapsamıyordu.**
> Şablon `deploy/examples/`'a taşınmıştı, orada en doğal hareket
> `cp secret.example.yaml secret.yaml`, ve o yol **COMMITTABLE**'dı. Artık kural tek
> dosya adı değil bir **desen**: `/deploy/**/*secret*.yaml` + `/deploy/*secret*.yaml`,
> `!*.example.yaml` istisnasıyla. Sekiz yazım IGNORED, iki şablon COMMITTABLE
> (`git check-ignore` **çıkış kodu** ile ölçüldü — `-v` çıktısı `!` satırını da
> bastığı için ilk okumam yanlıştı), ve gerçek bir dolu kopya `git add` tarafından
> **reddedildi**.
>
> **B3 — README operatöre yanlış bir komutu "güvenli" diye veriyordu.** *"`kubectl
> apply -f deploy/k8s/` artık güvenlidir (sekiz nesne)"* — sayı **dokuz** olmuştu ve
> *"güvenli"* Secret'tan fazlasını iddia ediyordu: o komut `tappa-db-init`
> ConfigMap'ini **üretmez** ve imaj etiketleri `:deploy-placeholder` kalır, yani boş
> kümede **asla açılmayan** bir kurulum, dolu kümede bir **kesinti** bırakır — üstelik
> orkestratörün az önce sildiği şeyin tarifi. Cümle kaldırıldı; yerine ne yaptığını
> satır satır söyleyen bir tablo ve elle deploy için doğru sıra. **Bu, kartın kendi
> imza kusuru** (*sağlanmayan bir garantiyi ilan etmek*), bu kez runbook'ta.
>
> **Yedi ek madde:** **B4** `01-roles.sql`'deki kanıt **geçersiz bir ölçümdü**
> (konteyner içi loopback + `trust`); ağ ölçümüyle değiştirildi ve neden geçersiz
> olduğu yazıldı. **B5** Cloudflare kapısı her curl hatasında sessizce geçiyordu;
> artık curl çıkış kodu **6 (çözülemedi)** ile diğerlerini ayırıyor — ölçüldü:
> çözülemeyen 0, çözülüp ulaşılamayan **1**, proxy'li **1**, temiz **0**.
> **B6** İki kapı da rollout'tan **sonra** koşuyordu; Cloudflare kontrolü artık
> **preflight** — hiçbir şey apply edilmeden önce, çünkü proxy'li bir host aksi halde
> canlı trafik alır ve §5'in *"ağ kanıtı"* notunu **değişmez** satırlara yazar.
> **B7** `MIGRATE_IMAGE` kapısız push ediliyordu — oysa DDL'i `tappa_owner` ile koşan
> o. `verify-image.sh --migrate` eklendi; buildinfo işe yaramaz (goose kendi
> modülünü damgalar), o yüzden **yük** kapılanıyor: imajdaki migration kümesinin
> sha256'sı ağaçtakiyle aynı olmalı. Gerçek imaj **geçiyor** (20/20,
> `023f5ff74fcdff16…`); iki kontrol de **ateşliyor** — fazladan bir dosya, ve *aynı
> sayıda dosyayla tek bayt* değişikliği. **B8** Kapı runner'ın pinlenmemiş Go'suna
> bağlıydı; `actions/setup-go@v5` + `1.26.6` eklendi (birinci parti, ci.yml'in
> kuralı). **B9** ExternalSecret örneği hâlâ **canlı adı** taşıyordu
> (`creationPolicy: Owner` ile canlı Secret'ı devralırdı); adı ve `target.name`
> örnek değere çevrildi. **B10** Üç pod spec'ine de
> `automountServiceAccountToken: false`.
>
> **C5** `deploy/README.md` PAT'i artık `read -rs` ile alıyor (kabuk geçmişi), ve
> panel doğrulama komutları parolayı ekrana basmak yerine konteynerin kendi
> `$POSTGRES_PASSWORD`'ünü kullanıyor. **C6** `actions/checkout@v4` etiket pini
> **bilinçli**: `ci.yml` reponun kuralını (Q25(b)) zaten yazıyor ve `checkout`
> GitHub'ın kendi runner'ında GitHub'ın kendi action'ı; `kubectl` ise ağdan inen
> üçüncü parti bir ikili, o yüzden **sha256 pinli**. Farklı tehdit, farklı muamele —
> gerekçe `deploy.yml`'de yazılı.

---

## M8-03 — Gözlemlenebilirlik

- **Bağımlılık:** M8-02
- **Kırmızı çizgi:** §4.7 · §7
- **Commit:** `feat(ops): add structured logging and alerts`

**Kabul kriterleri.**
- `log/slog`, yapılandırılmış, request id korelasyonlu.
- **Asla loglanmayanlar** doğrulandı: oturum token'ı, `token_hash`, CMAC, AES
  anahtarı, davet kodu, tam GPS koordinatı.
- Uyarı kuralları: `reject` oranı sıçraması, `flag` kuyruğu birikmesi, deaktive
  hesap denemesi, şüpheli ctr sıçraması, 5xx oranı.
- Prod log'ları AB'de, saklama süresi belirli.

---

## M8-04 — Güvenlik denetimi

- **Bağımlılık:** M8-01
- **Kırmızı çizgi:** §4 (tamamı)
- **Commit:** `chore(security): address pre-pilot audit findings`

**Kabul kriterleri.**
- agent `tappa-security-auditor` tam repo üzerinde koştu; R1–R8 için kanıtlı rapor.
- `make audit` temiz: `govulncheck` + `scripts/redline-check.sh`.
- KRİTİK ve YÜKSEK bulgu **kalmadı**; ORTA/DÜŞÜK olanlar ya kapandı ya
  gerekçesiyle kabul edildi ve yazıldı.
- Manuel doğrulamalar: replay denemesi gerçek etiketle, çapraz-tenant erişim
  denemesi, oturum çalma senaryosu, oran sınırı.
- Mekanik taramanın **kanıt olmadığı** unutulmadı — derin denetim ayrı yapıldı.

---

## M8-05 — Plaket encode runbook

- **Bağımlılık:** M2-01 · Q06 · Q10
- **Kırmızı çizgi:** §4.7
- **Commit:** `docs(ops): document tag encoding runbook`

**Amaç.** Fiziksel plaketleri üretime hazır hale getirmek.

**Kabul kriterleri.**
- Encode ayarları yazılı: UID mirror + counter mirror açık, SDM MAC input offset,
  dosya okuma izni açık, **yazma izni anahtarla kilitli**.
- Geliştirme akışı: 10'luk NTAG 424 DNA paketi (~30 €) + NXP TagWriter.
- **En az bir fiziksel etiketle uçtan uca doğrulama yapıldı** — test vektörleri
  yeşil ama gerçek çip kırmızı olabilir; bu adım atlanamaz.
- Anahtar teslimi ve döndürme prosedürü (tedarikçiden gelen anahtarlar üretimde
  değiştirilir).
- Anahtarlar repoya, sohbete, e-postaya **yazılmadı**; KEK ile sarmalı DB'de.
- Plaket baskısı: A5, NFC + QR birlikte, kamera görüş alanına montaj notu.

---

## M8-06 — KF St Julians pilotu

- **Bağımlılık:** M8-02 … M8-05 · Q13
- **Commit:** `docs: record pilot results`

**Amaç.** Tek şubede, 1 hafta, **BioTime ile paralel** çalıştırma.

> **PİLOT KAPISI (Q23) — hepsi ✓ olmadan pilot başlamaz.** Pilot gerçek
> çalışanların gerçek konum ve IP verisiyle koşacak:
> - [ ] GDPR Art. 13 çalışan aydınlatma metni yayında ve aktivasyonda gösteriliyor ([M5-02](m5-tap-akisi.md))
> - [ ] Gizlilik politikası, hizmet şartları, imprint yayında ([M7-01](m7-portal.md))
> - [ ] Tappa↔KF veri işleme sözleşmesi (DPA) imzalı ([M8-02](m8-deploy-pilot.md))
> - [ ] Saklama süresi ve silme/anonimleştirme akışı karara bağlı (Q13)
> - [ ] Telefon envanteri çıkarıldı ([M8-07](m8-deploy-pilot.md))
> - [ ] Üretim tenant'ı kuruldu ve doğrulandı ([M8-07](m8-deploy-pilot.md))
>
> "BioTime'ın GDPR yükünden kurtulun" diye satılan bir ürünün kendi pilotunda
> aydınlatma metni olmadan çalışması, ürünün ana satış argümanını çürütür.

**Kabul kriterleri.**
- Pilot kapısındaki altı madde de ✓.
- 15 kişilik kadro aktive edildi; her çalışanın practice tap'i tamamlandı.
- 1 hafta boyunca iki sistem paralel; günlük kayıt karşılaştırma tablosu.
- Uyuşmazlıkların her biri açıklandı (kim, ne zaman, neden) — açıklanamayan
  fark **kalmadı**.
- `flag` oranı ve nedenleri raporlandı — politika `sid` kırılımıyla (M3-07).
- **IP eşleşme oranı ölçüldü (Q14).** Kaç tap mekânın ağından geldi, kaçı mobil
  veriyle? Oran düşükse `proof of place` pratikte GPS'e indirgenmiş demektir;
  trust ağırlıkları (50/30) ve `base:gps-only-allow` varsayılanı buna göre
  yeniden değerlendirilir.
- **GPS-only tap oranı** ve WiFi adımını atlayan çalışan sayısı raporlandı.
- Çalışan geri bildirimi toplandı (ekran anlaşıldı mı, kaç saniye sürdü).
- Q13 (GDPR saklama/silme politikası) karara bağlandı ve yazıldı.
- Sonuç: iki firmaya tam yayılım kararı → BioTime kapatılır.

**Tuzaklar.**
- Pilot sırasında kod değiştirmek karşılaştırmayı bozar. Kritik hata dışında
  değişiklik pilot bitiminde.

---

## M8-07 — Üretim tenant kurulumu ve cihaz envanteri

- **Bağımlılık:** M7-03 · M8-02
- **Kırmızı çizgi:** §4.5 · §4.7
- **Commit:** `docs(ops): document production tenant setup`

**Amaç.** Pilotun üzerinde koşacağı **gerçek** tenant'ı kurmak ve sahadaki
telefonların ne yapabildiğini önceden bilmek.

**Neden.** Denetimde çıktı: M1-10 seed'i açıkça **sahte** (dokümantasyon IP'leri,
sahte AES anahtarları). M8-06 "15 kişilik kadro aktive edildi" diyor ama gerçek
KF tenant'ının hangi araçla, hangi adımlarla kurulacağı hiçbir kartta yoktu.

**Adımlar.**
1. KF tenant'ı M7-02 kayıt sihirbazından **gerçek veriyle** açılır (seed ile değil).
2. 9 lokasyon, gerçek statik IP'ler ve GPS koordinatları girilir; Rusty Bar
   `overnight = true`.
3. Her lokasyonun WiFi SSID'si lokasyon kaydına girilir ([M5-02](m5-tap-akisi.md)
   aktivasyon adımında çalışana gösterilecek).
4. 15 çalışan profili girilir; davetler gönderilir.
5. Plaketler encode edilir ([M8-05](m8-deploy-pilot.md)) ve UID'ler lokasyonlara bağlanır.
6. **Telefon envanteri:** kadroya tek soruluk anket — telefon modeli. Amaç:
   arka planda NFC okuyamayan cihazları (iPhone X ve öncesi) saymak.

**Kabul kriterleri.**
- Üretim tenant'ında **hiçbir** seed verisi yok; sahte AES anahtarı, dokümantasyon
  IP'si veya `91AC7E55…` tarzı örnek UID bulunmuyor.
- Statik IP'ler müşteriden **teyitli** (ISS yazısı veya test tap'i ile doğrulandı).
- NFC okuyamayan telefon oranı biliniyor. Yüksekse ([M5-08](m5-tap-akisi.md)):
  o çalışanlar kalıcı olarak QR yolunda demektir ve Q15 gereği IP zorunlu —
  WiFi adımı onlar için kritik, gerekirse tenant bazında politika gözden geçirilir.
- Gerçek bir plaketle, gerçek bir telefonla **en az bir uçtan uca tap** yapıldı
  ve kaydı panelde görüldü.
- Kurulum adımları runbook'a yazıldı — ikinci tenant (KM) aynı adımlarla açılacak.

**Tuzaklar.**
- Gerçek statik IP'leri ve müşteri verilerini **repoya yazma** (§4.7 ruhu;
  `tappa-seed` skill'i dokümantasyon aralıklarını şart koşuyor).
