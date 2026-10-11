# ADR 0027 — Holding: aynı holding'deki iki tenant arasında dokunuş ve görünürlük — CLAUDE.md §4.5'in ikinci bilinçli istisnası

- **Durum:** kabul edildi — **uygulama: yok (M11).** Bu ADR yazıldığında (HEAD `bcbff6d`, dal
  `m11-api`) şemada `holdings`, `holding_members`, `transactions.site_*` sütunları, `site_punches`,
  `tappa_holdingdefiner` rolü ve tek bir `holding_*` fonksiyonu **yoktur**; bir tenant'ın oturumuyla
  başka bir tenant'ın plaketine dokunmak `sys:tenant-mismatch` ile reddedilir ve hiçbir yere kayıt
  yazılmaz (aşağıda *"Bağlam"*). Uygulama görevleri: API-1a, API-1b, API-1c ve API-2'nin holding
  yarısı ([m11-entegrasyon-api.md](../plan/m11-entegrasyon-api.md) §5).
- **Tarih:** 2026-10-11. Plan 2026-10-09/10'da on bir denetim turundan geçti; 11. sürüm (`480df19`)
  üçüncü göz ve güvenlik merceğinden **ONAY** aldı, onaylı plan `bcbff6d`.
- **Karar veren:** kullanıcı (ürün sahibi) — **K-1** (2026-10-10: *"KF ve KM aynı holding altında
  ayrı iki tenant; holding bağıyla çapraz dokunuş"*), **K-1a, K-1b, K-1c, K-1d, K-1f, K-1g, K-1h**
  (2026-10-10); **K-1e**'yi kullanıcı önce kabul etti, aynı gün **kaldırdı**. Otonom kararlar
  (öneri uygulandı — gerekçeleri aşağıda, tablo ve bölümlerde): K-29, K-30, K-31'in holding yarısı,
  K-33, K-34, K-35, K-36, K-38, K-39 ve K-24'ün holding cümlesi. Bu ADR'nin yazımı sırasında yapılan
  iki ölçüm ve onlardan çıkan iki uygulama kuralı (§3, *"kilit çapası"* ve *"çakışma hedefi"*)
  yapıcının otonom kararıdır ve öyle işaretlidir.
- **Etkilenen (M11'de doğacak):** goose migration'ları (API-1a, API-1b, API-1c, API-2) ·
  `scripts/db-init/01-roles.sql` (`tappa_holdingdefiner`) + canlı küme runbook'u ·
  `internal/policy/guardrails.go` (#1'in eşlik şartı, yeni `sys:cross-tenant-qr-site-ip`) ·
  `internal/domain/tap` (`SameHolding`, saha kanıtı) · `internal/domain/checkin` (`advance`,
  `gather`, `write`) · `internal/handler/tap.go`, `checkin.go`, `brandlogo.go` · işverenin dokuz
  panel okuyucusu (§9) · `internal/db/holding.go` (elle, tipli — `resolve.go` emsali) ·
  `internal/handler/operator/` (holding ekranı) · CLAUDE.md §4.5, §5, §6, §9
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) md.6, md.7 ·
  [ADR 0004](0004-policy-motoru-modeli.md) §4–§6 · [ADR 0006](0006-debounce-iki-kosullu-zaman.md) ·
  [ADR 0007](0007-guardrail-sirasi-ve-guvenlik-uyarisi.md) ·
  [ADR 0008](0008-practice-satiri-ve-yon-zinciri.md) ·
  [ADR 0011](0011-duzeltme-satiri-yalnizca-kisaltir.md) ·
  [ADR 0016](0016-tenant-kapsamsiz-operator-icerigi.md) §2 ·
  [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) (ilk istisna — `op_*`) ·
  [ADR 0023](0023-tenant-markasi-ve-arayuz-kurali.md) · [ADR 0024](0024-kullanici-yukledigi-gorsel.md) §5 ·
  [ADR 0025](0025-tenant-askiya-alma.md) · [ADR 0026](0026-aktivasyon-ilk-nfc-dokunusunda.md) ·
  [ADR 0028](0028-dis-api-anahtar-akis-webhook.md) (dış API — punch olayları, imleç, holding
  tetikleyicisinin olay yarısı) · CLAUDE.md §4.3, §4.4, §4.5, §4.6, §5, §6, §9

**Atıf kuralı.** *"Plan §N"* = [m11-entegrasyon-api.md](../plan/m11-entegrasyon-api.md)'nin
bölümü; kırmızı çizgiler hep *"CLAUDE.md §4.x"*; çıplak *"§N"* bu ADR'nin bölümüdür. **Adlar:** KF
(Kebab Factory) ve KM (Kebab Manufacturing) aynı holding'in iki tenant'ı; **C** holding dışı bir
tenant; **H2** C'nin üyesi olduğu ikinci bir holding. Örneklerin çoğu *"KM çalışanı KF plaketinde"*
yönündedir; kural iki yönde aynıdır (KF@KM'de işveren KF, saha KM'dir).

## Tek cümlede

🔴 **Bu, CLAUDE.md §4.5'in BİLEREK aşıldığı İKİNCİ yerdir** (ilki `op_*`, ADR 0021): aynı
holding'in üyesi iki tenant'tan birinin çalışanı ötekinin plaketinde okutabilir; dokunuşun kaydı
**işverende** (oturumun tenant'ı) yazılır, kararı **işverenin** politikası verir, plaketin tenant'ı
(**saha**) yalnız kapalı bir izdüşümü (`site_punches`) görür; tenant sınırı **yalnız** §2'deki
**kapalı** `holding_*` fonksiyon listesinde ve holding tetikleyicilerinde aşılır, holding üyeliğini
**yalnız** `op_*` yazar; ve iki yönlü tehdit modeli normatiftir — **holding dışı bir tenant bu
yüzeye hiçbir yoldan erişemez** (tek ve sınırlı istisna: üyeliği yeni biten bir tenant için K-29'un
60 sn'lik çıkış penceresi — §6, §12), **eş bir tenant holding dışı bir anahtarı (plaket uid'i,
lokasyon id'si) veremez** (§12). Listeyi, bir fonksiyonun dönüş sütunlarını ya da definer'ın sütun
matrisini genişletmek **kullanıcı onayı + bu ADR'nin değişikliği** ister ve bir katalog testi bunu
mekanik olarak sabitler (§2).

## Neden bir ADR

- **CLAUDE.md §10:** şema (`holdings`, `holding_members`, `site_*`, `site_punches`), karar motoru
  (guardrail #1'in eşlik şartı, yeni guardrail) ve güvenlik sınırı (tenant'lar arası okuma ve
  yazma) — üçü birden değişiyor.
- **ADR 0002 md.6:** *"Süper-admin / çapraz-tenant erişim MVP'de yoktur. Böyle bir ihtiyaç doğarsa
  ayrı bir rol ve ayrı bir ADR ile gelir. `tappa_app` bu amaçla asla ayrıcalıklandırılmaz; ona
  `BYPASSRLS` verilmez."* ADR 0021 o ADR'nin birincisiydi; bu **ikincisidir**. md.6'nın iki koşulu
  korunur: rol **ayrı** (`tappa_holdingdefiner`, §2) ve `tappa_app` hiçbir tabloda tenant sınırını
  aşan bir yetki almaz — yalnız md.7 kalıbında, dar ve anahtar parametreli definer fonksiyonlarında
  `EXECUTE` alır (§2 tablosu).
- **ADR 0021'in "TEK" ifadesi** (başlık, *"Tek cümlede"*, §3.1) bu ADR'den sonra yanlıştır; ADR 0021'e
  M11 notu düşüldü, metni değişmez.
- **Neden `op_*` sınıfı değil:** `op_*`'ı yalnız `tappa_operator` çağırır; ADR 0021 *"Elenen
  seçenekler"* `op_*`'ın `EXECUTE`'unu `tappa_app`'e vermeyi açıkça eler (müşteri uygulamasının her
  SQL kusuru operatör yüzeyine ulaşırdı). Holding yolu ise tam olarak **müşteri** yoludur (tap,
  panel, API) — ayrı bir sınıf, ayrı bir sahip rol, ayrı bir patlama yarıçapı.
- **Neden ADR 0002 md.7 çözümleyicisi değil:** çözümleyici tenant bağlamını **üretir** (anahtar →
  tenant), sonra her şey o bağlamda koşar. `holding_*` ise bağlamı bilerek **aşar**: KM bağlamında
  KF'nin satırını okur (`holding_tap_site`) ve yazar (`holding_advance_tag_counter`).

## Bağlam — bugün (ölçüldü)

| Olgu | Kaynak |
|---|---|
| KF ve KM iki ayrı tenant. KM oturumu + KF plaketi → `sys:tenant-mismatch` (guardrail #1, `redirect`) → 403 problem ekranı, **hiçbir tenant'a kayıt yok** | `internal/policy/guardrails.go:306-319`, `internal/handler/checkin.go:227-233` (plan §1) |
| `checkin.advance` başka bir tenant'ın oturumuyla gelen dokunuşta plaketin sayacını **ilerletmez** (fonksiyonun *"dördüncü koşulu"*) | `internal/domain/checkin/checkin.go:927-974` |
| Aktivasyon yalnız davetin tenant'ının aktif plaketinde; yabancı plaket sayaç ilerlemeden reddedilir | ADR 0026; `internal/handler/activate.go:797,828` |
| Canlı oturum + bekleyen aktivasyon + yabancı plaket bugün aktivasyon hatasına döner | `internal/handler/tap.go:369-385` |
| Logo rotası yalnız oturum tenant'ınınkini verir; sonuç ekranında plaket başka tenant'ınsa logo yok | `internal/handler/brandlogo.go:240-245`, `internal/handler/checkin.go:320-323` |
| Saat dilimi bir **tenant** sütunudur ve owner panelden değiştirebilir | `db/queries/tenants.sql:158-164` |
| `unassigned` plaketin `location_id`'si NULL | `db/migrations/00013_harden_tags_and_add_inventory.sql:338-339` |
| QR satırları da `tag_uid` taşır | `internal/domain/checkin/checkin.go:1282-1285` |
| Veritabanında yalnız iki definer ailesi var: altı çözümleyici (`tappa_resolver`) ve `op_*` (`tappa_opdefiner`) | ADR 0021 §6; `internal/db/resolve.go` |
| Üretimde `pg_default_acl` = `tappa_app=arwd/tappa_owner` (yeni tablo dört fiille doğar); taze CI'da `ar` | plan §4.0; ADR 0021 *"Bağlam"* |
| **Yeni (bu ADR'nin sondası):** **`SELECT … FOR SHARE` (ve `FOR KEY SHARE`), kilitlenen tabloda en az bir sütunda `UPDATE` yetkisi ister.** Yalnız `SELECT` sütun yetkisi tutan BYPASSRLS bir definer'ın gövdesinde `FOR SHARE` → `permission denied`; aynı rol yalnız **ilgisiz bir sütunda** `UPDATE` tutunca → kabul | Bu ADR'nin sondası, 2026-10-11, atılmış bir PostgreSQL 17.10 kabında (paylaşılan geliştirme veritabanına dokunulmadı; kap silindi). Sonucu §3 *"kilit çapası"* |
| **Yeni (bu ADR'nin sondası):** **`INSERT … ON CONFLICT (<hedef>) WHERE … DO NOTHING`, hedef sütunlarda `SELECT` ister;** hedefsiz `ON CONFLICT DO NOTHING` yalnız `INSERT` ile geçer | Aynı sonda (kısmi `UNIQUE … NULLS NOT DISTINCT` indeksiyle). Sonucu §3 *"çakışma hedefi"* |
| **Yeni (1. tur düzeltmesi, 2026-10-11):** kilitli `SELECT` volatil olmayan bir fonksiyonda reddedilir (`SELECT FOR SHARE is not allowed in a non-volatile function`); `RETURNS TABLE` imzasının katalog metni typmod'suzdur (`char(14)` → `character`, `numeric(9,6)` → `numeric`, `time` → `time without time zone`) | Aynı yöntemle atılmış bir PostgreSQL 17.10 kabında. Sonucu §2: `holding_peer` VOLATILE; imza tablosunun yazımı |

## Karar

### Kullanıcı kararları

| # | Soru | Karar (kullanıcı, ürün sahibi) | Bedeli — adlandırılmış |
|---|---|---|---|
| **K-1** | Şirketler arası dokunuş (CLAUDE.md §4.5) | **KF ve KM aynı holding altında ayrı iki tenant; holding bağıyla çapraz dokunuş** (2026-10-10) | CLAUDE.md §4.5'e ikinci, adı konmuş istisna (yalnız holding içi); §2'nin kapalı fonksiyon listesi. **Kazanç:** API anahtarı = tenant (ADR 0028), EM-12 ön koşul değil, KM plaketleri yerinde, markalar ayrı |
| **K-1a** | Çapraz dokunuşun kaydı kimin? | **İşverenin (KM):** KM politikası karar verir, KM yöneticisi onaylar, manuel düzeltme KM'de; KF salt okur | KF'nin şube istisnaları KM çalışanına uygulanmaz (istisna K-1g); KM yöneticisinin KF sahası için yazdığı **manuel kayıt** KF'ye `employer_manual` olarak düşer ve KF itiraz edemez → KF işgücü raporu bunları **toplamdan hariç, ayrı** gösterir (DPA notuna) |
| **K-1b** | Aktivasyon hangi plakette? | **Yalnız kendi tenant'ının** — ADR 0026 değişmez | Yalnız KF'de çalışan bir KM çalışanı da ilk kez bir **KM** plaketine dokunmalı |
| **K-1c** | KM çalışanı KF plaketinde — tap ve sonuç ekranı | **KF markasıyla** (logo + accent; sonuç ekranında logo) | CLAUDE.md §9, ADR 0023/0024 notları; logo baytı `holding_logo` ile (§2) |
| **K-1d** | Holding içi görünürlük | **Kabul, DPA notuyla:** ad, kod, zaman, mekân; GPS, IP, not, policy bağlamı, e-posta **asla** | DPA / aydınlatma metni (saklama süresi dahil) kullanıcıda |
| ~~**K-1e**~~ | KF'de başlayıp KM plaketinde biten vardiyanın kapanış çıkışı KF'ye gitsin mi? | ~~Evet, yalnız zaman~~ → **KALDIRILDI** (2026-10-10): KF yalnız **kendi plaketlerindeki** dokunuşları görür | 7.–9. turun ORTA'larının çoğu ondan doğdu: kapanış bağı sütunu, ADR 0011 çatışması, mükerrer ve reddedilmiş kapanış, ve 9. turda ölçülen **her tenant'ta kayıt kaybı** (geriye beyanlı çıkış ve geriye dönük manuel düzeltme yazılamıyordu — CLAUDE.md §4.6). Kaldırılınca `closes_transaction_id`, `closes_shift` ve manuel yol değişikliği plandan çıktı, görünürlük daraldı. **Bu ADR'de K-1e'den türeyen hiçbir mekanizma yoktur.** |
| **K-1f** | Çapraz punch'ta karşı tarafın kimlikleri | **Paylaşılır:** çalışan `externalRef`/kod, lokasyon `externalRef`/ad, `tagId`, şirket kodu | K-1d'nin genişlemesi; e-posta/GPS/IP/not yine asla. Dış akıştaki biçimi ADR 0028 §4 (anlık görüntü, K-32) |
| **K-1g** | Çapraz QR okutması | **Sistem kuralı: o şubenin IP'si şart**, yoksa `flag`; işveren kapatamaz | Yeni `sys:` guardrail (§7) → CLAUDE.md §5, ADR 0004/0007 notları |
| **K-1h** | Holding içi görünürlüğün genişlemesi | **Kabul** (2026-10-10): KF kendi plaketindeki KM dokunuşunun **sonucunu** (sayıldı / onay bekliyor / onaylandı / reddedildi — **nedeni değil**) ve **yönünü** (giriş/çıkış) görür; rehber **aktif/pasif** durumunu verir (spec şartı) | KF, KM'de işten ayrılanı dolaylı öğrenir (tersi de); DPA notuna eklenir |

### Otonom kararlar — bu ADR'nin konusu olanlar (öneri uygulandı)

| # | Karar | Gerekçe (ölçüm / tur) | Yeri |
|---|---|---|---|
| K-29 | Holding üyeliği tenant kapsamsız iki tabloda, yazarı yalnız `op_*`; **kanal × zaman modeli**; dört sonuçlu ilerletme; `SameHolding`'in tek kaynağı; toleransın veritabanında zorlanması; `FOR SHARE` + `statement_timestamp()` | 6. tur: *"5 dk pencere"* yalnız erteliyordu; 7. tur: tek toleranslı pencere NFC'de sahte *"replay"*, QR'da çıkarılmış tenant'a 60 sn yeni yazma, okumada `static_ips` açıyordu; 11. tur: ortak saat commit edilmemiş `left_at`'i göremiyordu | §1, §5, §6 |
| K-30 | Kayıt **işverende**; `transactions`'a üç nullable `site_*` sütunu; `transactions` RLS'i değişmez; CHECK **kanala göre** | 6. tur: *"üçü dolu"* CHECK lokasyonsuz KF plaketinin reddini ve KF sahası için manuel düzeltmeyi yazılamaz kılıyordu (CLAUDE.md §4.6); 7. tur: FK'ler MATCH SIMPLE olduğu için `site_tenant_id` NULL bir satır C'nin `site_*`'ını taşıyabiliyordu | §4 |
| K-31 | **Tek holding tetikleyicisi önce çalışır**; iki sayaç satırını kanonik sırada **var eder**, **tek ifadede** kilitler, kilitlenen sayının 2 olduğunu assert eder; holding punch'ının iki hedef olay satırını da o yazar | 6. tur (iki mercek): iki ayrı tetikleyici → kilitlenme; 7. tur: tek ön-kilit 14 288 işlemde 0 kilitlenme, ama sayaç satırı **eksikken** `FOR UPDATE` onu atlayıp kilitlenmeyi geri getiriyordu | §8; olay yarısı ADR 0028 §4–§5 |
| K-33 | **Holding süzgeci** — anahtarın tenant'ı ∈ eşler(GUC tenant'ı) ∧ GUC tenant'ı bir holding'in üyesi; **iki yönlü test**, H2 fikstürü, M1/M2/M3 mutantları; **tek yüklem** `holding_peer` | 6. tur: tek yönlü test hatalı süzgeci geçiriyordu; 7. tur: C hiçbir holding'de değilken M3 iki testi de geçiyordu; 8. tur: tetikleyicideki M3 biçimli kontrol KM'nin manuel kaydına C'nin lokasyonunu yazdırıyordu | §2 |
| K-34 | `tappa_holdingdefiner` için **fiil × sütun matrisi** ve **"asla SELECT"** listesi, katalogda pinli | BYPASSRLS rolde fonksiyon yolunun **tek yapısal sınırı** sütun yetkileridir (7. turda prototipte 18 sütun ölçüldü) | §3 |
| K-35 | Canlı oturumlu ve bekleyen aktivasyon bağı olan tarayıcıda, plaketin tenant'ı davetinkinden **farklı** ve oturumun eşiyse normal holding dokunuşu; aynıysa aktivasyon tamamlanır | `tap.go:369-385` aksi hâlde onu yabancı plaket aktivasyon hatasına çevirirdi | §5 |
| K-36 | Holding üyesi tenant'ın saat dilimi değiştirilemez (tetikleyici, kapalı listede); ekleme farklı saat dilimini reddeder | 6. tur: owner sonradan değiştirirse çapraz geç kalma hesabı bozulurdu | §1 |
| K-38 | K-1g guardrail'ının yeri: `sys:person-debounce`'tan **hemen sonra**, baseline'dan önce; terminal | Debounce QR'ın tek frenidir (CLAUDE.md §5); önüne geçerse tekrar QR'lar onay kuyruğunu doldururdu | §7 |
| K-39 | K-1d'nin **ters yönü**: `holding_tap_site`'ın `static_ips`/GPS'i yalnız sunucu içi karar girdisidir, KM'ye gösterilmez; KM panelinde `site_*` satırları için IP/GPS ayrıntısı gösterilmez | 7. tur | §9 |
| K-24 (holding cümlesi) | E-posta tekilliği **holding genelinde aranmaz** | Holding genelinde aramak yeni bir tenant-ötesi okuma olurdu. Artık risk §13'te | ADR 0028 §9 |

### 1. Holding varlığı — tenant kapsamsız iki tablo, yalnız `op_*` yazar (K-29, K-36)

**Tablolar.**

```
holdings        (id, name, created_at)
holding_members (holding_id, member_tenant_id, joined_at, left_at, lock_anchor)
```

`lock_anchor`'ın neden var olduğu §3'tedir (yalnız `FOR SHARE`'in yetki şartı; anlamı yok, değeri
CHECK ile sabittir). `holding_members`'a gövdesinde değinen tek holding fonksiyonu `holding_peer`'dir
(§2); yazarlar yalnız aşağıdaki üç `op_*`'tır. Bu iki tablo **tenant kapsamsızdır** ve CLAUDE.md §6'nın o sınıf için yazdığı
dört kurala birebir uyar:
- **R5 muafiyetiyle görünür** (`-- redline: no-tenant-scope(holdings)`, aynısı `holding_members`
  için) — `scripts/redline-check.sh` her koşuda WARN basar (ADR 0016 §1 emsali);
- `tappa_app`'ten **açık** `REVOKE ALL` (üretimin `arwd` varsayılanı yeni tabloya dört fiil verir);
- `ENABLE` **ve** `FORCE ROW LEVEL SECURITY` birlikte (`scripts/pg-restore-verify.sh` ikisinin
  sayısı farklıysa geri yüklemeyi reddeder);
- **`tenant_id` adlı sütun yok** — üyenin kimliği `member_tenant_id`'dir, çünkü R5 ve RLS testleri
  `tenant_id` adını tenant tablosu işareti sayar.

`tappa_app` iki tabloda da **dört fiilde yetkisizdir** (üretimin `arwd` varsayılanı simüle edilerek
ölçülür); okuma yalnız §2'nin fonksiyonlarından, `holding_peer`'in kipleri üzerinden olur.

**Değişmezler.**
- **Aynı anda tek üyelik — çıkış penceresi dahil** (1. tur düzeltmesi, üçüncü göz). Bir tenant'ın
  `left_at IS NULL` **ya da** `left_at + 60 sn > clock_timestamp()` olan bir üyeliği varken yeni
  üyelik reddedilir (`op_add_holding_member` içinde, aşağıdaki satır kilidi altında). Çıkan tenant
  ancak eski üyeliğin `left_at`'i **ve** K-29'un 60 sn'lik toleransı geçtikten sonra **yeniden
  katılabilir** (yeni satır; eski satır tarihtir). Böylece toleranslı okumada (`holding_peer`'in
  `tolerant_stmt` kipi, §2) hiçbir tenant'ın iki üyeliği aynı anda görünmez — bir dokunuşun hangi
  holding'e ait olduğu hiçbir an iki anlamlı değildir. `op_add_holding_member` `holding_peer`'i
  çağıramadığı için (sahibi başka rol; `EXECUTE` yalnız `tappa_holdingdefiner`'da) 60 sn'nin ikinci
  kopyasını taşır; iki kopyanın eşitliği bir testle pinlenir.
- **`left_at` geri alınamaz:** bir kez yazılır, ne NULL'a döner ne ileri/geri oynar. Uzatma yoktur;
  geri dönüş yeniden katılımdır.
- **Zaman damgaları duvar saatinden:** `joined_at`, `left_at` ve her karşılaştırma
  `clock_timestamp()` (ADR 0021 §2 vii); tetikleyicilerin karşılaştırmaları ise
  `statement_timestamp()` ile (§6 — iki tetikleyici aynı ifadede aynı saati görsün diye).

**Yazarlar — yalnız üç `op_*`** (ADR 0021 sınıfı: `tappa_opdefiner` sahipli, yalnız
`tappa_operator` çağırır, ilk argüman operatör oturum hash'i, dönüş `void`/`uuid`, durum kehaneti
yok — ADR 0021 §2 v 7, B14):
- `op_create_holding` — boş holding açar.
- `op_add_holding_member` — tenant'ı ekler; **saat dilimi holding'in mevcut üyelerininkinden
  farklıysa reddeder** (K-36; K-7: raporlar tenant saat dilimiyle hesaplanır, çapraz geç kalma iki
  tenant'ın aynı saati okumasını ister). **Karşılaştırmadan önce kilitler** (1. tur, üçüncü göz
  ölçtü: kilitsiz karşılaştırma ile ekleme arasına sahibin saat dilimi `UPDATE`'i girebiliyordu):
  ilgili `tenants` satırlarını **kanonik sırada** (küçük id önce) — holding'in mevcut üyelerininkini
  `FOR SHARE`, eklenen tenant'ınkini `FOR NO KEY UPDATE` (`FOR SHARE`'den güçlü: aynı tenant için iki
  eşzamanlı ekleme de sıraya girer — tek üyelik kuralının kilidi budur; iki `FOR SHARE` birbirini
  beklemezdi). Sahibin saat dilimi `UPDATE`'i satırı `FOR NO KEY UPDATE` ile tuttuğu için iki kilitle
  de çakışır: ya ekleme bekler ve READ COMMITTED yeniden okumasıyla **yeni** saat dilimini görüp
  reddeder, ya da `UPDATE` bekler ve K-36 tetikleyicisi (aşağıda) commit edilmiş üyeliği görüp
  reddeder. FK denetimlerinin `KEY SHARE`'i bu kilitlerle çakışmaz — dokunuşlar beklemez. Kilitler
  yeni bir yetki istemez: `tappa_opdefiner`'ın 00034'ten beri `tenants`'ta tuttuğu `UPDATE
  (vat_verified, vat_checked_at)` satır kilidinin yetki şartını (*"Bağlam"*) karşılar; o yetki bir
  gün kaldırılırsa kilit `permission denied` ile düşer (fail-closed).
- `op_remove_holding_member` — `left_at`'i yazar (geçmiş bir an değil; çağrı anının duvar saati).

**Audit — her eylemin günlükleri, tek tek** (1. tur düzeltmesi, üçüncü göz; ADR 0021 K6 kalıbı,
ADR 0025 emsali; hepsi aynı işlemde):

| Eylem | `operator_audit_log` | tenant `audit_log`'u |
|---|---|---|
| `op_create_holding` | evet | **yok** (holding'in henüz üyesi yok) |
| ilk `op_add_holding_member` | evet | eklenen tenant |
| ikinci (ve sonraki) `op_add_holding_member` | evet | eklenen tenant **ve** holding'in o anki her diğer üyesi (v1: iki üye) |
| `op_remove_holding_member` | evet | çıkan tenant **ve** kalan her üye (v1: bir) |

Plan §4.1'in *"`operator_audit_log` ve iki tenant'ın `audit_log`'u"* ifadesi ikinci eklemenin ve
çıkarmanın hâlidir. v1 holding'i iki tenant'lıdır (plan §8: ikiden fazla tenant'lı holding için ek UI
kapsam dışı; veri modeli sınırlamaz, *"her diğer üye"* kuralı N üyeyi taşır).

**Saat dilimi kilidi (K-36).** `tenants` üzerinde `BEFORE UPDATE` bir tetikleyici
(`holding_timezone_lock`, `tappa_holdingdefiner` sahipli, kapalı listede — §2), `WHEN
(OLD.timezone IS DISTINCT FROM NEW.timezone)`: `holding_peer(NEW.id, NULL, 'is_member')` tenant'ın
canlı ya da 60 sn'lik çıkış penceresindeki bir üyeliğini bildiriyorsa değişikliği reddeder (pencere
yeniden katılım kuralıyla aynı: toleranslı kollardan pencerede yazılan bir satır hâlâ eski saat
dilimiyle hesaplanır). Owner'ın panel yolu (`db/queries/tenants.sql:158-164`) bu
yüzden üye tenant için hata alır; panel bunu bir cümleyle söyler. **v1'de saat dilimini "iki üyede
birlikte" değiştiren bir `op_*` YOKTUR** (plan §4.1 yalnız üç üyelik yazarını adlandırır); ihtiyaç
doğarsa ADR 0021 sınıfında kendi kartıyla gelir ve kilidi geçme biçimi **`tappa_app`'in
yazabildiği bir sinyale** (ör. özel bir GUC) dayanamaz. Bu tetikleyicinin sorusu bir eşlik sorusu
değildir (tek tenant: *"bir üyeliği var mı"*) ama yine `holding_peer`'in bir kipidir: `holding_members`'a
gövdesinde değinen tek holding fonksiyonu `holding_peer`'dir (§2).

### 2. Rol, fonksiyon sözleşmesi ve kapalı liste (K-33)

**Rol `tappa_holdingdefiner`:** `NOLOGIN BYPASSRLS NOSUPERUSER NOCREATEDB NOCREATEROLE`, **üyesiz**
(ne üyesi var ne bir rolün üyesi). Yalnız `scripts/db-init/01-roles.sql`'in idempotent bloğunda
doğar (OP-5'in `tappa_operator`/`tappa_opdefiner` kalıbı) ve canlı küme için `deploy/README.md`
runbook'unda yaratılır (kullanıcı işi; ADR 0021 §5); `pg_dump` rol taşımadığı için geri yükleme
provası **sıfırdan kurulmuş** bir pod'a yapılır. Migration'ın ön koşul bloğu rol yoksa ya da iki
yönden herhangi bir üyeliği varsa düşer (`db/migrations/00029_read_tenants_from_the_operator.sql:99-120` emsali). `internal/db/pool.go`'nun
rol reddi sertleştirilmiş ortamda `tappa_app` **herhangi bir** rolün üyesiyse açılışı reddeder
(ADR 0028 §4 — iki definer için ortak kapı).

**Neden ayrı bir rol** (`tappa_resolver` ya da `tappa_opdefiner` değil): patlama yarıçapları
birleşmez. `tappa_resolver`'ın yetkisi çözümleyici tablolarıdır (ADR 0002 md.7); `tappa_opdefiner`'ın
yetkisi operatörün geniş okumalarıdır ve onu `tappa_app` çağıramaz. Holding fonksiyonlarını
`tappa_app` çağırır; sahipleri bu ikisinden biri olsaydı bir holding kusuru o rolün yarıçapını
kazanırdı (ADR 0021 *"Elenen seçenekler"*, *"op_*'ların sahibini `tappa_resolver` yapmak"* ile aynı
gerekçe).

**Fonksiyon sözleşmesi — her `holding_*` definer'ı ve her holding tetikleyicisi:**
1. `SET search_path = pg_catalog, pg_temp` (`proconfig` sabit, katalogda pinli); bütün tablo adları
   `public.` nitelikli; **dinamik SQL yok**.
2. **Sabit dönüş sütunları** (`RETURNS TABLE(…)`; `SELECT *` yüzeyi yok) ve **LIMIT** (çok satır
   dönenlerde en çok 1000, sayfalı).
3. **Çağıranın tenant'ı GUC'tan:** `NULLIF(current_setting('app.tenant_id', true), '')::uuid` —
   parametre olarak **alınmaz**. Bağlam yoksa (NULL) cevap **0 satırdır**.
4. **Girdi yalnız sunucunun çözdüğü anahtardır** (plaket uid'i, sayaç değeri, logo özeti, imleç,
   `updatedSince`); istemcinin seçtiği bir tenant kimliği hiçbir fonksiyonun parametresi değildir.
5. **Süzgeç (K-33, normatif):** *anahtar satırının tenant'ı ∈ eşler(GUC tenant'ı) ∧ GUC tenant'ı bir
   holding'in üyesi* — ikisi **aynı** holding'de (`holding_id` eşitliği). Eş değilse **0 satır**,
   bilinmeyen anahtarla **aynı** cevap (kehanet yok — ADR 0026'nın *"aynı ekran ve durum kodu"*
   ilkesi).
6. **Gövdede RLS yoktur** (sahip BYPASSRLS) → açık süzgeç ve her ifadedeki açık tenant filtresi
   **tek bariyerdir** (ADR 0021 §3.2'nin aynısı).
7. **Hata yutan `EXCEPTION` bloğu yok** (savepoint geri alımı kilidi bırakır ve yazma sessizce
   düşerdi — ADR 0028 §4); eşlik bittiğinde bir dalı atlamak açık bir `IF`'tir, hata yutma değil.
8. **Ağ, uzun iş, uç nokta adresi okuma yok.**

**Tek yüklem — `holding_peer(a, b, mod)` (K-33).** *"İki tenant aynı holding'de eş mi"* sorusunun
**tek** cevabı budur; bütün `holding_*` fonksiyonları **ve iki tetikleyici** (BEFORE INSERT üyelik
kapısı, AFTER holding tetikleyicisi) onu çağırır. **1. tur düzeltmesinden (2026-10-11, üçüncü göz)
beri kural yapısaldır: `holding_members`'a gövdesinde değinen TEK fonksiyon `holding_peer`'dir**
(yazarlar olan `tappa_opdefiner` sahipli `op_*`'lar hariç) — satır içi eşlik kontrolü, onu
yazabilecek bir gövde kalmadığı için imkânsızdır. BEFORE kapısının kilidi ve K-36 kilidinin üyelik
sorusu da bu yüzden `holding_peer`'in kipleridir. `mod` **kapalı** bir kümedir (bilinmeyen kip →
hata):

| Kip | Soru | Saat | Çağıran |
|---|---|---|---|
| `strict` | `a` ve `b` aynı holding'de, iki üyelik de `left_at IS NULL OR left_at > clock_timestamp()` | `clock_timestamp()` | çağrılabilir `holding_*` fonksiyonları (`holding_advance_tag_counter`'da tek CTE'de **bir kez**) |
| `strict_stmt` | aynı soru, `statement_timestamp()` ile | ifade saati | BEFORE kapısının katı kolları (QR, manuel, ilerletilmemiş NFC); AFTER tetikleyicisinin onay kolu |
| `tolerant_stmt` | aynı holding, iki üyelik `left_at IS NULL OR left_at + 60 sn > statement_timestamp()` | ifade saati | **yalnız** BEFORE kapısının K-29 (i)/(ii) kolları (§6) |
| `lock` | `a` ve `b`'nin aday üyelik satırlarını (`left_at IS NULL OR left_at + 60 sn > statement_timestamp()`) kanonik sırada (küçük tenant id önce) **`FOR SHARE`** kilitler; karar vermez | — | **yalnız** BEFORE kapısı, karardan önce (§6) |
| `is_member` | `a`'nın (`b` NULL) `left_at IS NULL OR left_at + 60 sn > clock_timestamp()` olan bir üyeliği var mı | `clock_timestamp()` | **yalnız** K-36 saat dilimi kilidi (§1) |

`a = b` → `false` (bir tenant kendisinin eşi değildir; kendi verisi RLS'li yoldan gelir — rehber dahil,
ADR 0028 §2). 60 sn'nin tek tanımı `holding_peer`'dedir (`op_add_holding_member`'ın kopyası §1).
`holding_peer` **VOLATILE**'dir (`lock` kipi satır kilidi alır; PostgreSQL kilitli `SELECT`'i volatil
olmayan fonksiyonda reddeder — *"Bağlam"*), `holding_members` üzerindeki **tek satır kilidi
`FOR SHARE`'dir** — gövdesinde `FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR KEY SHARE`, `UPDATE`,
`INSERT`, `DELETE`, `MERGE` **yoktur** (1. tur, güvenlik merceği ölçtü: `lock_anchor`'ın `UPDATE`
yetkisi `FOR UPDATE`'e de izin veriyor ve ters sırada alınan `FOR UPDATE` kilitlenme üretiyor — §3).
**`prosrc` taraması (normatif):** `public`'teki her fonksiyon için `prosrc` `holding_members`
içeriyorsa ya adı `holding_peer`'dir ya da sahibi `tappa_opdefiner` ve adı `op_%`'dir; ve
`holding_peer`'in `prosrc`'unda yukarıdaki yasak deyimlerin hiçbiri geçmez. (Tarama bir ad
taramasıdır; bir görünümün arkasını görmez — `tappa_holdingdefiner`'ın üyelik okuyabildiği tek
nesnenin `holding_members` olması §3'ün matrisiyle tutulur.)
`holding_peer` **SECURITY INVOKER**, sahibi `tappa_holdingdefiner`, `EXECUTE` **yalnız sahipte**:
yalnız definer gövdelerinin içinden (orada `current_user` = sahip) çağrılabilir. `tappa_app`'e
asla — aksi hâlde serbest parametreli bir **üyelik kâhini** olurdu (9. tur ölçtü). 10. tur ayrıca
ölçtü: INVOKER olduğu için yanlışlıkla açılsa da `tappa_app` `holding_members`'ı okuyamaz. ADR
0021'in `prosecdef` sahip testi INVOKER'ı kapsamadığı için katalogda **ayrı bir satırla** pinlenir.

**Kapalı fonksiyon listesi — holding istisnasının tamamı.** Bu listede olmayan hiçbir kod yolu
tenant bağlamı olmadan bir tenant satırına dokunamaz (`op_*` hariç — ADR 0021). 🔴 **Listeye fonksiyon
eklemek, bir fonksiyonun dönüş sütunlarını değiştirmek ya da genişletmek, ya da `tappa_holdingdefiner`'ın
sütun matrisini (§3) genişletmek KULLANICI ONAYI + bu ADR'nin değişikliği ister** (tarihli not + iki
merceğin denetimi); argüman değişikliği yalnız bu ADR'nin değişikliğidir. *"Zaten N tane var"*
gerekçe değildir (ADR 0021'in md.7 kuralı). **Neden kullanıcı onayı (1. tur, güvenlik merceği,
ölçüldü):** dönüşü `static_ips`/GPS ile genişletilmiş bir `holding_site_locations` KM'ye KF'nin ağını
ve koordinatını döndürdü (K-39 ihlali) ve bu ADR'nin o güne kadarki hiçbir testi kırmızı olmadı — bir
dönüş sütunu, tenant sınırının ne kadar açıldığının kendisidir. Kural aşağıdaki **imza tablosu** ve
**katalog testiyle** mekanik olarak sabitlenir.

| Fonksiyon | Ne döner / ne yapar | Çağrı yeri | EXECUTE |
|---|---|---|---|
| `holding_tap_site(p_uid)` | Eş tenant'ın plaketi: `site_tenant_id`, plaketin durumu; varsa `location_id`, lokasyon adı, `external_ref`, `static_ips`, GPS, vardiya, `overnight`; saha tenant'ının saat dilimi ve `api_code`'u; marka meta (accent, logo boyutu ve logo `alt`'ı olan işletme adı — **logo özeti yalnız VIES-doğrulanmış** tenant için). `static_ips`/GPS yalnız sunucu içi karar girdisidir (K-39) | `GET /t` (sayfa: KF mekân adı + KF markası) · `checkin.gather` (kilitli işlem içinde — `SameHolding`'in kaynağı, §5) | `tappa_app` |
| `holding_advance_tag_counter(p_uid, p_ctr)` | **Tek ifade** (`db/queries/tags.sql:33-46`'nın CTE + `FOR UPDATE` kalıbı): eş şartı `prev`'de **ve** `UPDATE … WHERE`'de, **katı** üyelik (`left_at > clock_timestamp()`), üyelik **tek CTE'de bir kez** hesaplanır (`clock_timestamp()` iki kez değerlendirilmez). Dönüş `(sonuç, plaketin güncel durumu)`; sonuç dört değerden biri: eş değil → `not_peer` · eş ama plaket `active` değil → `peer_inactive` · eş, aktif, güncelleme boş → `replay` · güncelleme dolu → `advanced` (6.–10. turda ölçüldü: 20 eşzamanlı çağrıda tam 1 `advanced`, 19 `replay`) | **`checkin.advance`** — `sun.Verify`'a dokunulmaz (o yalnız aktivasyon yoludur) | `tappa_app` |
| `holding_logo(p_uid, p_digest)` | Eş tenant'ın **güncel** logo baytı ve türü; yalnız tenant VIES-doğrulanmışsa **ve** özet `holding_tap_site`'ın verdiğiyle aynıysa (eski ya da başka tenant'ın özeti → 0 satır) | logo rotası (ADR 0024 notu) | `tappa_app` |
| `holding_site_locations(p_cursor)` | Eş tenant'ların lokasyonları: lokasyonun tenant'ı (manuel kaydın `site_tenant_id`'si için anahtar), id, ad, `external_ref`, vardiya, `overnight` — sayfalı, en çok 1000. **`static_ips` ve GPS asla** (K-39) | KM panelinde manuel kayıt lokasyon seçimi; işverenin raporları (§9) | `tappa_app` |
| `holding_directory(p_updated_since, p_cursor)` | **Eş** tenant'ların çalışanları — **çağıranın kendi tenant'ı hariç** (kendi çalışanları her zaman RLS'li sorgudan gelir, ADR 0028 §2): kimlik, `api_code`, `external_ref`, kod, ad, soyad, **aktif/pasif** (K-1h) — **e-posta yok**; sayfalı, en çok 1000 | `GET /v1/directory` (ADR 0028) | `tappa_app` |
| `holding_peer(a, b, mod)` | İki tenant'ın **aynı** holding'de eş olup olmadığı (tek yüklem) ve yukarıdaki beş kip | yalnız bu listedeki definer'lar ve tetikleyiciler | **yalnız sahip** (INVOKER, VOLATILE) |
| Tetikleyici `holding_site_gate()`: **`site_*` üyelik kapısı** | `BEFORE INSERT ON transactions`, `WHEN (NEW.site_tenant_id IS NOT NULL)`; K-29'un kanal × zaman şartı (§6) | `transactions` | yalnız sahip |
| Tetikleyici `holding_feed()`: **holding akış/izdüşüm tetikleyicisi** | `AFTER INSERT ON transactions` ve `ON transaction_reviews`, tetikleyici adı **ad sırasında ilk** (K-31); `site_punches` (§9) ve iki hedefin olay/teslim satırları (§8; ADR 0028 §4) | `transactions`, `transaction_reviews` | yalnız sahip |
| Tetikleyici `holding_timezone_lock()`: **saat dilimi kilidi** | `BEFORE UPDATE ON tenants` (K-36, §1) | `tenants` | yalnız sahip |

**İmza tablosu (normatif — 1. tur düzeltmesi).** Yazım PostgreSQL'in `pg_get_function_identity_arguments`
ve `pg_get_function_result` metnidir (typmod'suz — *"Bağlam"*); ilk migration bu metni üretir ve
katalog testi birebir pinler. `tappa_holdingdefiner`'ın sahip olduğu fonksiyonların kümesi **tam
olarak** bu tablodur.

| Fonksiyon | Argümanlar | Dönüş |
|---|---|---|
| `holding_tap_site` | `p_uid character` | `TABLE(site_tenant_id uuid, tag_status text, location_id uuid, location_name text, location_external_ref text, static_ips cidr[], gps_lat numeric, gps_lng numeric, shift_start time without time zone, shift_end time without time zone, overnight boolean, timezone text, api_code text, accent character, logo_sha256 text, logo_mime text, logo_width integer, logo_height integer, business_name text)` |
| `holding_advance_tag_counter` | `p_uid character, p_ctr integer` | `TABLE(outcome text, tag_status text)` — `outcome ∈ {advanced, not_peer, replay, peer_inactive}` |
| `holding_logo` | `p_uid character, p_digest text` | `TABLE(logo bytea, logo_mime text)` |
| `holding_site_locations` | `p_cursor uuid` | `TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text, shift_start time without time zone, shift_end time without time zone, overnight boolean)` |
| `holding_directory` | `p_updated_since timestamp with time zone, p_cursor uuid` | `TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text, first_name text, last_name text, active boolean, updated_at timestamp with time zone)` |
| `holding_peer` | `a uuid, b uuid, mod text` | `boolean` |
| `holding_site_gate` | — | `trigger` |
| `holding_feed` | — | `trigger` |
| `holding_timezone_lock` | — | `trigger` |

`static_ips` ve GPS **yalnız** `holding_tap_site`'ın dönüşündedir ve yalnız sunucu içi karar girdisidir
(K-39); başka hiçbir dönüşte yoktur.

**Katalog testi — `TestHoldingCatalog_TheFunctionsAndTheirExactSignatures`** (emsal
`internal/db/operatortenants_test.go:388`, `TestOperator00029_TheFunctionsAndTheirExactSignatures`):
(i) sahibi `tappa_holdingdefiner` olan fonksiyonların **ad kümesi** imza tablosuna eşittir — eksik
ya da fazla bir ad kırmızı; (ii) her birinin argüman ve dönüş metni tablodakiyle **birebir**; (iii)
ad başına **tek aşırı yükleme** (ikinci bir imza ikinci bir kapıdır); (iv) adı `holding\_%` olan
**her** fonksiyonun sahibi `tappa_holdingdefiner` (ters yön — sahipliği unutulmuş bir `holding_*`
superuser sahipli bir definer olurdu); (v) `prosecdef` `holding_peer` dışında `true`, `holding_peer`
için `false` ve `provolatile = 'v'`; `proconfig` = `search_path=pg_catalog, pg_temp` (tetikleyicilerde
ayrıca `lock_timeout`); (vi) `EXECUTE` ACL'i yukarıdaki tablonun sütunu (PUBLIC ve `tappa_resolver`
için `false`). Dönüşü genişletilmiş bir `holding_site_locations` (1. turun mutantı) bu testte
kırmızıdır.

**EXECUTE ve bağlama kuralları (plan §4.0):** M11'in her fonksiyonunda `REVOKE ALL ON FUNCTION … FROM
PUBLIC`; çağrılabilir `holding_*` fonksiyonlarında `EXECUTE` **yalnız `tappa_app`**'e; tetikleyici
fonksiyonlarında `EXECUTE` **yalnız sahipte** ve gövde **`TG_RELID`**'i (bağlandığı tablonun
beklenen tablo olduğunu) assert eder. Gerekçe ölçüldü: 4. turda `tappa_app`'in PUBLIC'ten aldığı
`TEMP` hakkıyla kendi geçici tablosuna bir tetikleyici fonksiyonunu bağlayıp `NEW`'i kendisi
belirleyebildiği; 6. turda BYPASSRLS sahipli bir tetikleyici fonksiyonunun `EXECUTE`'u `tappa_app`'e
verilince **öksüz satır** yazılabildiği görüldü. `prorettype = trigger` *"doğrudan çağrılamaz"*
demektir, *"bağlanamaz"* değil.

**Go tarafı:** `holding_*` çağrıları `internal/db/holding.go`'da **elle ve tipli** yazılır —
sqlc v1.28 `RETURNS TABLE(…)` döndüren fonksiyon çağrısını tipleyemiyor (ADR 0002 md.7, üç ayrı
ölçüm); `resolve.go` emsali. Ayrı bir `internal/holding` paketi **yoktur** (karar ve izdüşüm
veritabanında, yol `checkin`'de; ayrı paket yalnız ince bir sarmalayıcı olurdu). KM oturumundan
**hiçbir zaman** `WithTenant(KF)` açılmaz (ADR 0016 §2 korunur): KF'nin satırına giden tek yol bu
fonksiyonlardır.

### 3. Fiil × sütun matrisi ve "asla SELECT" listesi (K-34)

BYPASSRLS bir rolde fonksiyon yolunun **tek yapısal sınırı** sütun yetkileridir: kusurlu bir gövde
ancak yetkisi olan sütunu okuyabilir. **Tetikleyici yolunda** gövde `NEW`'i yetkiden bağımsız okur;
orada asıl bariyer holding tetikleyicisinin **yazdığı her tablonun** sütun kataloğudur (aşağıda
*"yazılan tablolar"*) — o tablolarda serbest metinli ya da kanıt sütunu yoktur.

**Matris (normatif; `has_table_privilege` / `has_column_privilege` ile katalogda pinlenir; listede
olmayan her tablo × fiil × sütun = yetki yok).** *(M11)* işaretli sütunlar API-1/API-1b/API-1c/API-2
ile doğar.

| Tablo | SELECT (yalnız bu sütunlar) | INSERT | UPDATE | DELETE | Neden |
|---|---|---|---|---|---|
| `holding_members` | `holding_id`, `member_tenant_id`, `joined_at`, `left_at` | — | **yalnız `lock_anchor`** | — | `holding_peer`; `FOR SHARE` (aşağıda *kilit çapası*) |
| `holdings` | — | — | — | — | hiçbir gövde okumaz (`holding_id` eşitliği `holding_members`'ta) |
| `tags` | `uid`, `tenant_id`, `location_id`, `status`, `last_ctr` | — | `last_ctr` | — | `holding_tap_site`, `holding_advance_tag_counter` (`FOR UPDATE` bu `UPDATE` yetkisiyle karşılanır), BEFORE kapısının `last_ctr ≥ NEW.ctr` şartı |
| `locations` | `id`, `tenant_id`, `name`, `external_ref` *(M11)*, `static_ips`, `gps_lat`, `gps_lng`, `shift_start`, `shift_end`, `overnight` | — | — | — | `holding_tap_site`, `holding_site_locations`, anlık görüntü |
| `tenants` | `id`, `name`, `timezone`, `vat_verified`, `api_code` *(M11)*, `api_enabled_at` *(M11)* | — | — | — | saha meta, VIES kapısı, anlık görüntünün şirket kodu, teslim kapısı (ADR 0028 §6); saat dilimi kilidi `NEW`/`OLD`'u okur |
| `tenant_branding` | `tenant_id`, `accent`, `logo_sha256`, `logo_mime`, `logo_width`, `logo_height`, `logo` | — | — | — | marka meta; `logo` baytı yalnız `holding_logo` gövdesinde |
| `employees` | `id`, `tenant_id`, `external_ref` *(M11)*, `employee_code` *(M11)*, `first_name` *(M11)*, `last_name` *(M11)*, `full_name`, `status`, `updated_at` *(M11)* | — | — | — | `holding_directory`, `site_punches.employee_name`, anlık görüntü |
| `transactions` | `id`, `tenant_id`, `employee_id`, `site_tenant_id`, `site_location_id`, `site_tag_uid` *(M11)*, `channel`, `type`, `verdict`, `practice`, `occurred_at`, `ctr`, `sun_valid` | — | — | — | onay yolunda bağlı satırın şekli (`transaction_reviews`'ın `NEW`'i yalnız kimlik taşır) |
| `webhook_endpoints` *(M11)* | `id`, `tenant_id`, `active` | — | — | — | saha teslim satırı (ADR 0028 §6) — **adres ve anahtar asla** |
| `site_punches` *(M11)* | çakışma hedefi sütunları (aşağıda) | §9'daki sütun listesi | — | — | izdüşüm |
| `punch_events` *(M11)* | — | ADR 0028 §4'teki sütun listesi | — | — | iki hedef olay satırı (K-31) |
| `webhook_deliveries` *(M11)* | — | ADR 0028 §6'daki ekleme sütunları | — | — | iki hedef teslim satırı |
| `feed_counters` *(M11)* | `tenant_id`, `last_seq` | `tenant_id`, `last_seq` | `last_seq` | — | K-31'in *"var et"* + kilit + artırma (`ON CONFLICT … DO UPDATE … RETURNING` SELECT ister — 3. tur) |

`USAGE ON SCHEMA public` (bu kümede PUBLIC'in `USAGE`'ı yok — 4. tur ölçtü). Listede olmayan
tablolar (`sessions`, `admin_users`, `admin_sessions`, `employee_invites`, `password_resets`,
`audit_log`, `policies`, `platform_*`, `operator_*`, `api_keys`, `api_idempotency`,
`invite_requests`, …) için rol **hiçbir fiil** tutmaz. **Matris kesin listenin üst sınırıdır:**
API-1a/API-1b/API-1c/API-2 her satırı ölçerek yeniden kazanır ve gereksiz çıkan sütun düşer
(`tappa_feedwriter`'ın 25/26 kalemlik *"her biri tek başına zorunlu"* kanıtının emsali — ADR 0028 §4).

**"Asla SELECT" listesi (K-34 — katalogda `has_column_privilege('tappa_holdingdefiner', …, 'SELECT')
= false` ile tek tek pinlenir):** `tags.aes_key_ref`, `tags.app_key_ref`, `employees.email`,
`admin_users.email`, `platform_admins.email`, `platform_admins.totp_secret_sealed`,
`webhook_endpoints.url`, `webhook_endpoints.signing_key_sealed`, `transactions.gps_lat`,
`transactions.gps_lng`, `transactions.source_ip`, `transactions.note`,
`transactions.policy_context` ve **adı `_hash` ile biten her sütun** (katalog sorgusuyla, ad
listesiyle değil — bugün `admin_users.password_hash`, `sessions.token_hash`,
`admin_sessions.token_hash`, `password_resets.token_hash`, `employee_invites.code_hash`,
`employee_invites.consent_binding_hash`, `platform_admins.password_hash`,
`platform_admins.enroll_token_hash`, `platform_sessions.token_hash`,
`operator_read_tickets.ticket_hash`; M11'de `api_keys.token_hash`). **Bu ADR'nin eki (plan
listesinin ilkesinden — *"serbest metinli sütun yok"*):** `transaction_reviews.note` da listededir.

**Yazılan tabloların sütun pini (tetikleyici yolunun bariyeri):** holding tetikleyicisinin yazdığı
dört tablo — `site_punches`, `punch_events`, `webhook_deliveries`, `feed_counters` — katalogda
**tam sütun listesiyle** pinlenir; hiçbirinde GPS, IP, not, `policy_context`, `verdict`,
`matched_sid`, e-posta ya da serbest metin sütunu yoktur. Yeni bir sütun bu testi kırmızıya çevirir.

**Kilit çapası — `holding_members.lock_anchor` (yapıcının ölçümü ve kararı, 2026-10-11).** K-29
BEFORE kapısının iki üyelik satırını **`FOR SHARE`** ile kilitlemesini ister. PostgreSQL `FOR SHARE`
için kilitlenen tabloda **en az bir sütunda `UPDATE`** yetkisi arar (*"Bağlam"*: ölçüldü). Bu yetkiyi
bir **üyelik** sütununda (`left_at` …) vermek, K-29'un *"üyeliği yalnız `op_*` yazar"* değişmezini
yetki düzeyinde deler. Bu yüzden: `holding_members.lock_anchor boolean NOT NULL DEFAULT false CHECK
(lock_anchor = false)`; `tappa_holdingdefiner`'ın `holding_members` üzerindeki **tek** `UPDATE`
yetkisi bu sütundadır; CHECK onu anlamsız kılar (yazılabilecek tek değer zaten olan değer — `true`
yazmak `23514`, ölçüldü), üyelik sütunlarında `has_column_privilege(…, 'UPDATE') = false` pinlenir ve
hiçbir holding gövdesi `holding_members`'a `UPDATE` yazmaz (`prosrc` taraması). Rol NOLOGIN ve
üyesiz olduğu için bu yetkiyi yalnız sabit gövdeler kullanabilir. **Ama yetki yalnız `FOR SHARE`'i
açmaz** (1. tur, güvenlik merceği ölçtü): aynı `UPDATE` yetkisi `FOR UPDATE` ve `FOR NO KEY UPDATE`'e
de izin verir ve ters sırada alınan bir `FOR UPDATE` kilitlenme üretir. Bu yüzden §2'nin `prosrc`
taraması `holding_members`'a değinen tek gövdeyi (`holding_peer`) ve oradaki tek satır kilidini
(`FOR SHARE`) pinler; `FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR KEY SHARE`, `UPDATE`, `MERGE` yoktur.
**Elenen:** `FOR SHARE` yerine
advisory kilit (K-29'un ölçülmüş `FOR SHARE` + READ COMMITTED yeniden okuma sıralamasını değiştirir —
plan değişikliği olurdu); üyelik sütununa `UPDATE` (değişmezi deler).

**Çakışma hedefi — `site_punches`'ın iki kısmi UNIQUE'i (yapıcının ölçümü ve kararı).** Red
tekilleştirmesinin iki eklemesi `ON CONFLICT … DO NOTHING`'dir (§9). Hedefsiz `ON CONFLICT DO NOTHING`
yalnız `INSERT` ile geçer ama **her** tekillik ihlalini yutar (birincil anahtar dahil) — toplam
eşlemede sessiz düşmenin kapısı olurdu. Bu yüzden iki ekleme **hedefli** yazılır (`ON CONFLICT
(<indeks sütunları>) WHERE <indeksin yüklemi> DO NOTHING`) ve rol hedef sütunlarda `SELECT` tutar
(`tenant_id`, `site_tag_uid`, `ctr`, `employee_id`, `outcome`, `bucket_hour`, `sun_valid`) — ölçüldü:
hedefli biçim `SELECT`'siz `permission denied`. Bu sütunlar izdüşümün kendi sütunlarıdır; sır ya da
kanıt değildir (`sun_valid`'in `tappa_app`'ten saklanması K-1h içindir, §9).

### 4. Kayıt — `transactions.site_*` ve kanal CHECK'i (K-30)

`transactions`'a üç **nullable** sütun: `site_tenant_id`, `site_location_id`, `site_tag_uid`.
Satır yeniden yazılmaz (CLAUDE.md §4.3 — sütunlar `INSERT`'te dolar, `UPDATE` yolu yoktur);
`transactions`'ın RLS politikası **değişmez** (satır işverenin tenant'ındadır, `tenant_id` =
işveren).

- **FK'ler:** `(site_location_id, site_tenant_id) → locations(id, tenant_id)`,
  `(site_tag_uid, site_tenant_id) → tags(uid, tenant_id)`; `ON DELETE RESTRICT` (lokasyon silmenin
  son kemeri — §9). Kısmi indeks `WHERE site_tenant_id IS NOT NULL`.
- **CHECK — kanala göre (normatif):**
  - **tap kanalları** (`nfc`, `qr`): `site_tenant_id` ve `site_tag_uid` **dolu**; `site_location_id`
    **NULL olabilir** (lokasyonsuz / `unassigned` plaketin reddi yazılabilsin — CLAUDE.md §4.6);
  - **`manual`**: `site_tenant_id` ve `site_location_id` **dolu**, `site_tag_uid` **NULL**;
  - **her durumda**: `site_tenant_id <> tenant_id`; `site_tenant_id` doluysa `location_id` ve
    `tag_uid` **NULL** (yerel ve saha sütunları aynı satırda bulunmaz);
  - ve **`site_tenant_id IS NOT NULL OR (site_location_id IS NULL AND site_tag_uid IS NULL)`** —
    FK'ler `MATCH SIMPLE` olduğu için bu satır olmadan `site_tenant_id` NULL bir satır C'nin
    `site_*`'ını FK denetimsiz taşıyabiliyordu (7. tur ölçtü).
- **ALTER maliyeti:** 688 bin satırlık kopyada ~0,76 sn ölçüldü (6. tur). Prod boyutunda yeniden
  ölçülür; **eşik 2 sn**, aşılırsa CHECK/FK `NOT VALID` + ayrı `VALIDATE`.

### 5. Tap yolu (K-29, K-35, K-1a, K-1b)

- **`GET /t`:** oturum KM, plaket KF → `holding_tap_site` **1 satır** → sayfa **KF mekân adı + KF
  markası** (K-1c); **0 satır** → bugünkü davranış (yabancı plaket problemi). Canlı oturum + bekleyen
  aktivasyon bağı + eş plaket (plaketin tenant'ı davetinkinden farklı) → **normal holding dokunuşu**
  (K-35); plaketin tenant'ı davetinkiyle aynıysa aktivasyon yine tamamlanır. Aktivasyon değişmez
  (K-1b; ADR 0026; `activate.go:797,828`).
- **`checkin.advance`:** plaketin tenant'ı ≠ oturumunki → `holding_advance_tag_counter` (katı).
  Sonuç karara taşınır:
  - `not_peer` → bugünkü gibi **ilerletmeden** reddet (#1, **kayıt yok**, sayaç ilerlemedi);
  - `replay` → `sys:sun-invalid` reddi, `site_*` dolu kayıt;
  - `peer_inactive` → `sys:tag-not-active` (#2) reddi, `site_*` dolu kayıt (**403'e düşmez**);
  - `advanced` → holding dokunuşu.

  İlerletme plaketin **o anki** durumunu da döner; `advance` onu `Decide`'a verir — bayat
  `tagRow.Status` değil (`checkin.go:730`; 10. tur: ilerletme anında aktiften pasife geçen plaket).
  Holding ilerletmesi yalnız `status='active'` plaketi ilerletir (`checkin.go:919-921`'in değişmezi).
  **Eşzamanlılık:** aynı `(tag, ctr)` ile N goroutine, yarısı tek-tenant yarısı holding yolu → tam
  **1** başarı (CLAUDE.md §4.4; `-race`).
- **`gather`:** çalışan, departman vardiyası, son kayıt, debounce, son açık giriş **KM'de, bugünkü
  sorgularla** (6. tur kodda doğruladı: hepsi `(tenant, employee)` süzgeçli). Dokunulan lokasyonun
  kanıtı (IP aralıkları, GPS, vardiya) `holding_tap_site`'tan.
- **`SameHolding`'in tek kaynağı (K-29):** NFC'de ilerletme **denendiyse** dört sonucundan
  (`advanced` / `replay` / `peer_inactive` → eş; `not_peer` → değil); ilerletme **denenmediyse**
  (plaket aktif değil ya da CMAC geçersiz — `checkin.go:928-930`) ve **QR'da**, `gather`'daki **katı**
  `holding_tap_site`'tan. `advanced` olup `gather`'ın katı okuması **0 satır** dönerse (`left_at`
  arada geçti) karar **saha kanıtı olmadan** verilir (→ `flag`) ve kayıt **yazılır** — hata değil.
- **Karar:** politika kümesi **işverenin** (K-1a); `write`: satır işverende, `site_*` dolu. Guardrail
  #1 ve yeni guardrail §7'de.
- **Kayıp / emekli / lokasyonsuz KF plaketi:** işverende `reject` kaydı (`site_tenant_id`,
  `site_tag_uid` dolu, `site_location_id` NULL olabilir — K-30); KF bunu izdüşümde
  `refused_tag_not_active` olarak görür ve **KF panelinin uyarı yüzeyi** (bugün `AlertLostTagTapped`
  yalnız oturum tenant'ının audit'ine yazıyor) bu izdüşüm satırından beslenir. **Çapraz audit
  yazılmaz:** hiçbir yol başka tenant'ın `audit_log`'una yazmaz (`op_*`'ın K6 satırları hariç).
- **Sonuç ekranı:** KF logosu (K-1c; bugün farklı tenant'ta logo yok — `checkin.go:320-323`);
  sonuç ekranında accent yoktur (CLAUDE.md §9 değişmez).

### 6. Kanal × zaman modeli — üyelik bittiği an (K-29)

Üyelik `left_at` anında biter; soru, **o anı kesen** bir dokunuşun ne olacağıdır. Tek bir pencere
her kanalda yanlıştı (7. tur), bu yüzden kural kanal ve satır şekline göredir.

| Yol | Zaman kuralı | `left_at`'i kesen dokunuşun sonucu |
|---|---|---|
| NFC ilerletme (`holding_advance_tag_counter`) | **katı** (`left_at > clock_timestamp()`) | `left_at`'ten sonra → `not_peer` → 403, kayıt yok, sayaç ilerlemez |
| NFC yazma — ilerletilmiş dokunuş | **60 sn tolerans**, kol (i): `sun_valid IS TRUE` ∧ saha plaketinin `last_ctr ≥ NEW.ctr` | ilerletme `left_at`'ten önce, yazma sonra → holding kaydı **yazılır** |
| Red satırı (NFC **ve** QR) | **60 sn tolerans**, kol (ii): `channel IN ('nfc','qr')` ∧ `verdict='reject'` ∧ (`sun_valid IS NOT TRUE` ∨ saha `last_ctr ≥ NEW.ctr`) | replay, `peer_inactive`, kayıp plaket ve QR reddi (ör. deaktive çalışanın güvenlik uyarılı denemesi) karar ile yazma arasında `left_at` geçse de **kaybolmaz** (10.–11. tur ölçtü). Red satırı mesai doğuramaz: yön NULL'a zorlanır, yalnız `flag` onaylanır. Son şart `sun_valid=true` taşıyıp ileri `ctr` yazan bir ham yazıcıyı sınırlar |
| İlerletilmemiş NFC satırı (`last_ctr < ctr`, `sun_valid` yanlış, `ok`/`flag`) | tolerans yok | `left_at`'ten sonra INSERT'te **reddedilir** |
| QR karar ve yazma | **katı** | **adlandırılmış yarış:** katı karar ile INSERT arasında `left_at` geçerse INSERT reddedilir → *"tekrar dene"*; tekrarda karar `not_peer` → kayıt yok (QR'da sayaç yok, harcanan bir şey yok) |
| Manuel kayıt | **katı** | `left_at`'ten sonra saha için manuel kayıt reddedilir |
| Onaylar (`transaction_reviews`) | **katı** (`statement_timestamp()`) | onay işverende **her zaman** yazılır (işverenin kendi kaydı); eşlik bitmişse KF tarafı (izdüşüm, saha olayı) atlanır — §9 bedeli |
| Okumalar (`holding_tap_site`, `holding_logo`, rehber, lokasyon listesi) | **katı** | `left_at`'ten sonra 0 satır |

**Toleransın uygulandığı yer veritabanıdır.** BEFORE INSERT kapısı ve holding tetikleyicisinin
`transactions` kolu 60 sn toleransı **yalnız** yukarıdaki iki NFC/red koluna uygular; geri kalan
her şey katıdır. `sun_valid` kodda ilerletmenin başarısıdır (`checkin.go:966-970`).

**Sıralama — `FOR SHARE` ve `statement_timestamp()`.** BEFORE kapısı karar vermeden **önce**
`holding_peer(işveren, saha, 'lock')` ile iki tenant'ın aday `holding_members` satırlarını
**`FOR SHARE`** ile kilitler (kanonik sırada), sonra kolun kipiyle (`tolerant_stmt` ya da
`strict_stmt`) karar verir: bekleyen bir
`op_remove_holding_member` (`left_at`'i yazan, satırı `FOR NO KEY UPDATE` ile tutan) ile sıraya
girer ve READ COMMITTED'in yeniden okumasıyla **güncel** `left_at`'i görür (11. tur ölçtü: yalnız
ortak saat, commit edilmemiş `left_at`'i göremiyordu). Karşılaştırmalar `statement_timestamp()` ile
— BEFORE ve AFTER aynı ifadede **aynı** saati görür (10. tur: iki ayrı saat ölçüldü). **AFTER'ın
`transactions` kolu eşliği yeniden sormaz:** BEFORE'dan geçmiş bir `site_*` satırı eştir — kabul
kapısı BEFORE'dur; böylece *"BEFORE kabul etti, AFTER KF tarafını atladı"* satırı yoktur (FK
beklemesinde `left_at` geçen yarışta da, commit edilmemiş `op_remove` ile kesişen INSERT'te de).
`FOR SHARE`'in yetki şartı §3 *"kilit çapası"*.

**Tolerans ≥ 2 × istek zaman aşımı.** İstek zaman aşımı 30 sn (`internal/httpx/router.go:32,90`);
60 sn tolerans bunun iki katıdır ve bir testle pinlenir — zaman aşımı yükselirse test kırmızıdır.
8. tur ölçtü: ilerletilmiş 300 dokunuş kabul, ilerletilmemiş 5000 ret.

### 7. Karar motoru — guardrail #1'in eşlik şartı ve `sys:cross-tenant-qr-site-ip` (K-1g, K-38)

- **Guardrail #1 (`sys:tenant-mismatch`):** eşleşme şartı
  `TagTenantID ≠ SessionTenantID ∧ ¬SameHolding`. Holding dışı (ya da üyeliği bitmiş) bir tenant
  için bugünkü davranış aynen: `redirect`, kayıt **hiçbir** tenant'a yazılmaz. `SameHolding`
  §5'in tek kaynak kuralından gelir; motor onu **hesaplamaz**, verilen olgu olarak okur (ADR 0004
  notu).
- **Yeni guardrail `sys:cross-tenant-qr-site-ip` (K-1g):** kanal `qr` ∧ çapraz (`SameHolding`) ∧
  **¬(saha IP'si eşleşti)** → `flag` (effect `review`). Saha IP'si `holding_tap_site`'ın
  `static_ips`'inden ve CLAUDE.md §5'in *"bir mekânın ağının olabileceğinden geniş liste IP
  eşleşmesi sayılmaz"* kuralıyla (`internal/netx`) hesaplanır.
- **Yeri (K-38):** `sys:person-debounce`'tan (**#8**) **hemen sonra**, baseline'dan önce →
  guardrail listesinde **#9**; iki yetkilendirme guardrail'ı (`sys:policy-edit-owner-only`,
  `sys:no-self-review`) #10 ve #11'e kayar (başka bir eylem ad alanında eşleştikleri için tap
  kararını etkilemezler). Gerekçe: debounce QR'ın **tek frenidir** (CLAUDE.md §5); önüne geçseydi
  60 sn içindeki tekrar QR'lar `ignored` yerine `flag` olup onay kuyruğunu doldururdu. Deaktive
  çalışan (#5) ve zamanlama guardrail'ları (#6, #7) önünde kalır — ADR 0007'nin uyarı kuralı
  bozulmaz.
- **Terminaldir (adlandırılmış):** işveren onu **gevşetemez** (çapraz QR'da IP'siz `ok` yolu yok —
  KM `base:qr-requires-ip`'yi kapatsa bile) ve **`deny`'e de sıkılaştıramaz** (guardrail
  eşleşince baseline/tenant katmanı hiç çalışmaz — ADR 0004 §4). Bedeli kabul edildi.
- **Politika kümesi işverenin (K-1a):** KF'nin `location/<id>` kapsamlı istisnaları KM çalışanına
  uygulanmaz (plan §2 madde 25) — tek istisna yukarıdaki guardrail.
- **Debounce (ADR 0006)** kişi bazlıdır ve işverende hesaplanır; iki tenant'ın plaketlerini
  **birlikte** kapsar (KF'de dokunup 60 sn içinde KM plaketine dokunan → `ignored`).
- **Yön zinciri (ADR 0008)** işverendedir: KF'de giriş + KM'de çıkış → doğru `out`.
- **Güven puanı, geç kalma:** CLAUDE.md §5'in hesabı aynen; geç kalma **tap edilen** lokasyonun
  vardiyasına göre (departman vardiyası yoksa) — KF lokasyonunun vardiyası `holding_tap_site`'tan;
  K-36 iki tenant'ın aynı saat dilimini okumasını garanti eder.

### 8. Holding tetikleyicisi — sıra ve kilit (K-31)

Holding tetikleyicisi (`tappa_holdingdefiner`, BYPASSRLS) **ad sırasında ilktir**; kendi-tenant
tetikleyicisi (`tappa_feedwriter`, ADR 0028 §4) ikincidir. Ad sırası katalog testinde pinlenir.

1. **Önce eşlik kararı** (`holding_peer`, `statement_timestamp()`; onaylarda katı, `transactions`
   kolunda BEFORE'dan geçmiş olmak eşliktir — §6).
2. **Sonra — olay yazılacaksa — kanonik sayaç kilidi:** iki sayaç satırı (işveren ve saha) önce
   `INSERT … ON CONFLICT DO NOTHING` ile **var edilir** (küçük tenant id önce), sonra **tek ifadede**
   `… WHERE tenant_id IN (işveren, saha) ORDER BY tenant_id FOR UPDATE` ile kilitlenir ve
   **kilitlenen satır sayısının 2 olduğu** assert edilir. 7. tur ölçtü: satır eksikken `FOR UPDATE`
   onu atlıyor ve kilitlenme geri geliyordu; *"var et"* adımıyla 8 640 işlemde 0 kilitlenme
   (mutantta 324).
3. **Sonra satırlar:** işveren olayı sayacı artırdığı için kilit ondan **önce** alınmalıdır (aksi
   hâlde çift kilit ters sırada alınır — 6./7. turun kilitlenme sınıfı).

**Ne yazar** (`site_*` taşıyan satırlar ve onlara bağlı onaylar için; kendi-tenant tetikleyicisi bu
satırlara **hiç dokunmaz**):
- K-4 şartını sağlayan (ADR 0028 §4) her satırda işverenin **olay ve teslim** satırını **eşlikten
  bağımsız** yazar — her holding punch'ında işveren olayı **tam bir kez** (9. tur: işverenin teslim
  satırı yazarsız kalmıştı);
- eşlik sürüyorsa ayrıca saha olay ve teslim satırını yazar (iki hedef — ADR 0028 §4);
- **K-4'ten bağımsız olarak**, eşlik sürüyorsa `site_punches` satırını yazar (redler ve onay
  bekleyenler dahil — §9'un toplam eşlemesi);
- eşlik **bitmişse** işverenin olay satırını karşı taraf anlık görüntüsü **NULL** olarak yazar; KF
  tarafı atlanır, hata yok.

İşverenin olay satırındaki karşı taraf (saha lokasyonunun ref'i/adı/şirket kodu) **ancak burada**
okunabilir: NOBYPASSRLS `tappa_feedwriter` GUC=KM iken KF satırlarını göremez. Yazdığı saha
satırlarının `tenant_id`'si **yalnız** `NEW.site_tenant_id` olabilir (gövdede assert).

**Bedel — yeni tenant'lar arası bağımlılık (adlandırılmış):** KF'de sayaç satırını tutan takılı bir
işlem, KM'nin holding dokunuşlarını `lock_timeout` sonunda *"tekrar dene"*ye düşürür (sayaç
ilerlemiş, kayıt yok — bugünkü sınıf); `idle_in_transaction_session_timeout` bunu sınırlar (ADR
0028 §5).

### 9. Saha görünürlüğü — `site_punches` izdüşümü (K-1d, K-1h)

**Tablo.** Saha (KF) tenant'ında, **RLS beşlisiyle** (CLAUDE.md §6 — `tenant_id` = saha), append-only
(`UPDATE`/`DELETE`/`TRUNCATE` tetikleyicileri; `cmd/tappa/scriptguards_test.go`'nun `APPEND_ONLY`
listesine girer):

```
site_punches(tenant_id /* = saha */, id, transaction_id, employer_tenant_id, employer_api_code,
             employee_id, employee_ref, employee_code, employee_name, site_location_id,
             site_tag_uid, ctr, sun_valid, bucket_hour, occurred_at, channel, type, outcome,
             ctr_gap, created_at)
```

GPS, IP, not, policy bağlamı, `verdict`, `matched_sid`, e-posta **yok** — katalogla pinli.
`transaction_id` işverenin satırını adlandırır ama KF onu okuyamaz (RLS); yalnız tekilleştirme ve
*"son sonuç"* katlaması için bir anahtardır.

**`outcome` — kapalı küme (K-1h):** `counted` · `pending_review` · `approved` · `rejected` ·
`refused_tag_not_active` · `refused` · `employer_manual`. `verdict` ve `matched_sid`
**kopyalanmaz**: KF **sonucu** görür, **nedeni** görmez.

**Eşleme — tam ve toplam.** Her `verdict` × kanal × onay durumu için ya bir `outcome` ya da açık
bir *"yazılmaz"*:
- `ok` (nfc/qr) → `counted`; `flag` → `pending_review`; onay olayı → `approved` / `rejected` (onay
  satırı ana kaydın `occurred_at`'ini ve `type`'ını taşır);
- `reject` + `sys:tag-not-active` → `refused_tag_not_active`; diğer her `reject` → `refused`;
- `manual` (işverenin saha için yazdığı) → `employer_manual`;
- **`ignored` (60 sn tekrarı) → izdüşüme yazılmaz** — açık bir dal, hata değil: bir `CASE` hatası
  işverenin kaydını geri alırdı (CLAUDE.md §4.6). Katalog testi her hücreyi dolaşır; eşlenmemiş bir
  hücre kırmızıdır.

**Red tekilleştirmesi** (9.–10. tur: satırdaki `ctr` saldırganın yazdığı değerdi; replay ile sahte
CMAC satırda ayırt edilemiyor — ikisi de `sys:sun-invalid` ve `sun_valid=false`):
- **`sun_valid IS TRUE` olan redler** (gerçek, ilerletilmiş dokunuş — ör. deaktive çalışan) `ctr`
  ile: `UNIQUE NULLS NOT DISTINCT (tenant_id, site_tag_uid, ctr, employee_id) WHERE outcome LIKE
  'refused%' AND sun_valid IS TRUE`;
- **diğer bütün redler** (NFC'de `sun_valid` yanlış — replay dahil —, QR redleri) `ctr`'sız,
  **sunucu saatinden** saat kovasıyla: `bucket_hour timestamptz = date_trunc('hour', now(), 'UTC')`
  — tetikleyici `now()`'dan yazar (çağıranın yazabildiği `created_at`'ten de, istemcinin
  `occurred_at`'inden de değil); üç argümanlı `date_trunc` oturum saat diliminden bağımsızdır (11.
  tur: iki argümanlı biçim UTC ve Malta oturumlarında iki kova üretiyordu). `UNIQUE NULLS NOT DISTINCT
  (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) WHERE sun_valid IS NOT TRUE AND
  outcome LIKE 'refused%'` — NULL `sun_valid`/`employee_id` delikleri kapalı; **`outcome` anahtarda**
  (sahte bir `refused` kayıp plaket uyarısını bastıramaz);
- iki ekleme de **hedefli** `ON CONFLICT … DO NOTHING` (§3): işverende her red **yine yazılır**
  (CLAUDE.md §4.6), KF'ye (ve kayıp plaket uyarısına) en çok bir kez. 11. tur ölçtü: 200 sahte URL →
  1 satır, gerçek 3 → 3, işverende 240/240; eşliği bitmiş tenant'ın 60 sn penceresinde KF'ye düşen
  satır tekilleştirmeyle sınırlı (2 200 red → 44).

**`tappa_app` erişimi:** `site_punches`'ta **sütun listesiyle** `SELECT`, **`sun_valid` hariç**
(K-1h *"nedeni değil"*: `refused ∧ sun_valid` *"imza geçerli ama reddedildi"* demekti — 11. tur);
INSERT/UPDATE/DELETE yok. KF'nin bütün okumaları sıradan, RLS'li, açık `tenant_id` filtreli
sorgulardır — KF hiçbir `holding_*` fonksiyonunu izdüşüm için çağırmaz.

**KF raporu holding çalışanları için SÜRE HESAPLAMAZ** (10. tur ölçtü: izdüşüm işverenin
zincirindeki aradaki olayları görmediği için *"en geç giriş → en erken çıkış"* eşlemesi KF'de hiç
geçmemiş 9 sa / 32 sa aralıklar sayıyordu). Holding çalışanlarının dokunuşları zaman, yön ve
**son** sonuçla (`transaction_id` başına son satır) listelenir; süre sütunu *"işverende"* yazar.
KF'nin **kendi** çalışanlarının süreleri değişmez (KF ledger'ı yalnız kendi `transactions`'ını okur —
`ListWorkedShiftEvents`; ledger değişmez, yeni olan yalnız liste görünümü).
- `pending_review` KF'de **her zaman** *"sonuç işverende"* diye gösterilir — üyelik bittikten sonra
  verilen onay/ret KF'ye düşmez (adlandırılmış bedel; KF üyelik durumunu okuyamadığı için ikisi
  ayırt edilmez).
- `employer_manual` satırları **ayrı** ve **toplamdan hariç** (fiziksel kanıtı yok, KF itiraz
  edemez — K-1a bedeli).
- KF'nin kayıp plaket uyarısı ve plaket *"son görülme"* değeri `site_punches.created_at` (sunucu
  saati) ile beslenir — istemcinin beyan ettiği `occurred_at` ile değil (11. tur: tek bir sahte URL
  *"son görülme"*yi kalıcı olarak 2099'a çekebiliyordu).
- KF panelinde holding dokunuşları (liste), plaket son görülme ve **sayaç boşlukları** (`ctr_gap`)
  izdüşümden. **KF onay vermez** (plan §8: KF'nin KM çalışanı için onay vermesi kapsam dışı).

**İşverenin okuyucuları** (rol geneldir — KF@KM'de saha KM'dir): `transactions.location_id`/`tag_uid`
okuyan **dokuz** çağıran (7. tur saydı) `site_*` satırını doğru mekân ve vardiyayla göstermelidir;
kaynak `holding_site_locations`:
`ListPanelTransactions` · `ListFlaggedForReview` · `ListOpenCheckIns` (saha mekân adı) ·
`ListWorkedShiftEvents` + `resolveShift` (saha vardiyası; *"<şirket> — <mekân>"* kovası — bugün
`uuid.Nil` kovasına düşüyor, `report.go:838-841`; CSV dışa aktarım da `ledger.Hours` üzerinden bunu
kullanır) · `ListAnomalyVenues` · `ListTapsTakenTogether` (bugün dışarıda bırakıyor) ·
`ListAnomalyPlaques` (`anomaly.go:450`; saha plaketi işverenin anomalisi değildir → dışarıda, saha
bunu `ctr_gap` ile görür) · `ListTagLastSeen` (`plaque.go:674`; aynı) · `CountLocationReferences`
(saha tarafı izdüşümle). Faturalama, kadro ve operatör tarafında okuyucu yok.
**K-39:** işverenin panelinde `site_*` satırları için IP/GPS ayrıntısı **gösterilmez** (bugün de
seçilmiyor — `db/queries/transactions.sql:606-615`).

**Lokasyon silme:** KF'nin `CountLocationReferences`'ı KM'deki `site_location_id`'leri göremez →
sayım izdüşümden de yapılır; FK `RESTRICT` son kemer (§4).

**Manuel kayıt** yalnız işverende; KF sahası için `site_tenant_id` + `site_location_id` (lokasyon
seçimi `holding_site_locations`'tan), aynı BEFORE kapısından geçer (**katı** eşlik); KF'de
`employer_manual`. K-33: KM'nin manuel kaydına C'nin lokasyonu yazılamaz (8. tur ölçtü — tek
yüklemle kapandı).

### 10. Marka — KF plaketinde KF markası (K-1c)

Tap ekranı ve sonuç ekranı **plaketin** tenant'ının markasını taşır: tap ekranında logo + accent,
sonuç ekranında logo (accent yok). Kaynak `holding_tap_site`'ın marka meta'sı; logo baytı
`holding_logo(p_uid, p_digest)` ile, **yalnız VIES-doğrulanmış** saha tenant'ı için (ADR 0023 WL-13
kapısının aynısı) ve özet `holding_tap_site`'ın verdiğiyle aynıysa. Logo rotası (`GET
/t/logo/{sha}`) bugün yalnız oturum tenant'ınınkini veriyor (`brandlogo.go:240-245`); M11'de canlı
oturumun **eş** tenant'ının o anki plaketine ait özet de `holding_logo` ile servis edilir — C'nin ya
da eski bir özetin baytı **asla** (bayt-aynı 404, ADR 0024 §5). CLAUDE.md §9'un sayılı istisnası
genişlemez: slotlar aynı, yalnız **kimin** markası olduğu değişir.

### 11. Askı ile ilişki (ADR 0025)

Askı dokunuşu durdurmaz — holding dokunuşu dahil: askıdaki işverenin ya da askıdaki sahanın
plaketinde holding dokunuşu askısız gibi karar alır ve yazılır; tetikleyicilerin izdüşüm, olay ve
teslim yazması askıda **açıktır** (ADR 0028 §10). Holding üyeliği askıdan bağımsızdır (operatörün
ayrı eylemi).

### 12. Tehdit modeli — normatif

**Varlıklar:** KF'nin plaket sayaçları (`last_ctr` — replay koruması, CLAUDE.md §4.4), KF'nin
mekân kanıtı (`static_ips`, GPS — K-39), KF'nin logo baytı, iki tenant'ın çalışan kimlikleri ve
e-postaları, iki tenant'ın mesai kayıtları (CLAUDE.md §4.3, §4.6), holding üyeliğinin kendisi.

**Saldırganlar ve iddialar:**

1. **Holding dışı tenant C (oturumu, yöneticisi, API anahtarı) hiçbir yoldan erişemez — K-29'un
   60 sn'lik çıkış penceresi hariç** (1. tur düzeltmesi, güvenlik merceği). **Tek ve sınırlı
   istisna:** üyeliği **yeni biten** bir tenant, `left_at`'ten sonraki 60 sn boyunca K-29'un iki
   toleranslı kolundan (ilerletmesi `left_at`'ten önce yapılmış NFC dokunuşu; replay, `peer_inactive`,
   kayıp plaket ve QR **redleri**) eski eşin plaketine bağlı satır yazdırabilir; bu satırlar eski eşin
   izdüşümüne ve akışına düşer — red tekilleştirmesiyle sınırlı (11. tur: 2 200 red → 44). Yeni
   ilerletme, okumalar, QR kararı ve manuel kayıt pencerede de **katıdır**; pencere yeniden katılımı da
   kapatır (§1). Hiç üye olmamış bir C için istisna yoktur.
   - C oturumu + KF plaketi → `holding_advance_tag_counter` = `not_peer` → #1 → **403, kayıt yok,
     KF sayacı ilerlemez**; `holding_tap_site` 0 satır → sayfa bugünkü yabancı plaket problemi.
   - C bağlamında **her** `holding_*` fonksiyonu 0 satır (bilinmeyen anahtarla aynı cevap).
   - C'nin bir holding'e katılmasının tek yolu `op_add_holding_member`'dır (operatör, TOTP'li ayrı
     kimlik — ADR 0020); `tappa_app` `holding_members`'a hiçbir fiille dokunamaz (`arwd` simüle).
     `tappa_app`'in yazabildiği tek bir üyelik yolu, herhangi bir tenant'ın holding'e katılması
     demek olurdu (plan §6) — yok.
   - C, `holding_peer`'i çağıramaz (EXECUTE yalnız sahipte; INVOKER — çağırsa da `holding_members`'ı
     okuyamaz).
   - C'nin anahtarı `holding_directory`'yi (ADR 0028) çağırdığında 0 satır; C'nin `site_punches`'ı
     boştur (izdüşüm yalnız eşe yazılır).
2. **Eş tenant (KM) holding dışı bir anahtar veremez.**
   - KM oturumu + **C'nin plaketi** → `not_peer` → **403, kayıt yok, C'nin `last_ctr`'ı değişmez**
     (ilerletme ifadesinde eş şartı hem `prev`'de hem `UPDATE … WHERE`'dedir).
   - KM bağlamı + C'nin uid'i / lokasyon id'si → her fonksiyonda 0 satır.
   - KM'nin manuel kaydı + C'nin lokasyonu → BEFORE kapısında ret; C'nin `site_punches`'ı **0**.
   - Süzgeç *"anahtarın tenant'ı ∈ eşler(GUC tenant'ı)"*dır; üç hatalı biçim ölçülerek elenir:
     **M1** (yalnız çağıran üye mi), **M2** (yalnız anahtar üye mi), **M3** (ikisi de *bir*
     holding'in üyesi mi — `holding_id` eşleşmesi unutulmuş). **Fikstürde C ikinci bir holding'in
     (H2) üyesidir** — yoksa M3 iki testi de geçer (7. tur ölçtü). Üç mutantın her biri hem
     fonksiyonlarda **hem iki tetikleyicide** kırmızı olmalıdır (tek yüklem `holding_peer`
     sayesinde mutant tek yere uygulanır ve her yolu kırmızıya çevirir).
3. **Eş tenant'ın kullanıcıları — görünürlük K-1d/K-1f/K-1h ile sınırlı.** KF, KM çalışanının adını,
   kodunu, `externalRef`'ini, dokunuş zamanını, yönünü ve **sonucunu** görür; GPS, IP, not, policy
   bağlamı, e-posta, `verdict`, `matched_sid` ve (`tappa_app` için) `sun_valid` **asla** (katalog
   pinleri). KM, KF'nin `static_ips`'ini ve GPS'ini görmez (K-39); KM satırının `ip_match`'i ve
   `policy_context`'i KF'nin ağını/koordinatını **dolaylı** açabilir — adlandırılmış bedel (§13).
4. **Kendi tenant'ındaki ayrıcalıklı olmayan kod yolu (SQL kusuru, enjeksiyon) `tappa_app` olarak.**
   Erişebileceği en geniş yüzey: kapalı listedeki fonksiyonların sabit dönüşleri, yalnız eş
   tenant'ın anahtarlarıyla. Tetikleyicileri bağlayamaz (EXECUTE yalnız sahipte + `TG_RELID`
   assert'i), `holding_peer`'i çağıramaz, `holding_members`'ı okuyamaz.
5. **Replay (CLAUDE.md §4.4).** Holding ilerletmesi `ctr` ifadesinin **ikinci kopyasıdır**; aynı
   tek-ifade, atomik biçimde ve `tags_counter_monotonic` kısıtının tek başına eşit `ctr`'yi
   durdurmadığı bilinerek çekişme testiyle sınanır (aynı `(tag, ctr)`, iki yoldan N goroutine → 1).
   R4 tarayıcısı şema nitelikli `UPDATE public.tags SET last_ctr` biçimini görecek şekilde
   genişler (ADR 0028 §3).
6. **Kayıt kaybı (CLAUDE.md §4.6).** Holding yolundaki hiçbir ret, tolerans ya da tekilleştirme
   işverenin kaydını düşürmez: kanal CHECK'i lokasyonsuz plaket reddini ve saha manuel kaydını
   yazılabilir kılar (K-30); red kolu `left_at` yarışında da yazar (K-29); `ignored` izdüşüme açık
   bir dalla yazılmaz (hata yolu yok); tekilleştirme yalnız KF tarafını daraltır.

**Kapatılmayanlar — sayılı:** (a) **operatör** C'yi bir holding'e ekleyebilir — bu yetki tasarım
gereği operatörde (ADR 0020/0021'in tehdit modeli: ele geçirilmiş operatör tenant sınırını zaten
aşar); (b) **BYPASSRLS gövdede mantık hatası** — sütun matrisi okunabilecek sütunları sınırlar,
satırları değil; satır sınırı açık süzgeçtir ve onu üç mutantlı iki yönlü test tutar (ADR 0021
sınır 2'nin aynısı); (c) holding'in iki tenant'ı arasında **kasıtlı** veri paylaşımı K-1d/f/h'nin
kendisidir — DPA kullanıcıda; (d) **üyelik sürerken `tappa_app`'teki bir SQL kusuru** (enjeksiyon,
hatalı sorgu) işverenin `transactions`'ına **fiziksel kanıtı olmayan** bir `site_*` dokunuş satırı
yazabilir (ör. `verdict='ok'`, `sun_valid=true`, sahte `ctr`): BEFORE kapısı eşliği ve K-29 kolunu
sınar, dokunuşun gerçekliğini değil. **Sınırı:** satır yalnız **işverenin çalışanı** (RLS `WITH
CHECK`) × **eşin plaketi ya da lokasyonu** (bileşik FK'ler + `holding_peer`) olabilir; sahte satır eşin
izdüşümüne ve iki tenant'ın akışına düşer. Aynı kusur bugün işverenin **kendi** tenant'ında sahte bir
kayıt yazabiliyor (sınıf yeni değil); yeni olan, sonucun **eşin görünürlüğüne** ulaşmasıdır —
adlandırılmış kalan risk (1. tur, güvenlik merceği; §13 madde 13).

### 13. Bedeller ve kabul edilen riskler (adlandırılmış)

1. **CLAUDE.md §4.5'e ikinci istisna** — kapalı liste + imza tablosu + iki yönlü test + sütun
   pinleri ile sınırlı; listeyi, bir dönüşü ya da matrisi genişletmek kullanıcı onayı + bu ADR'nin
   değişikliğidir (§2).
2. **K-1a:** KF'nin şube istisnaları KM çalışanına uygulanmaz (çapraz QR hariç — K-1g).
   `employer_manual` satırlarına KF itiraz edemez; raporda ayrı ve toplamdan hariç.
3. **K-1b:** yalnız KF'de çalışan bir KM çalışanı aktivasyon için bir KM plaketine dokunmalı.
4. **K-1d/K-1f/K-1h:** ad, kod, ref, zaman, yön, sonuç ve aktiflik holding içinde paylaşılır; KF,
   KM'de işten ayrılanı dolaylı öğrenir (tersi de). DPA / aydınlatma metni ve `site_punches`'ın
   saklama süresi kullanıcıda (plan §7 madde 5).
5. **K-39 artığı:** işverenin kendi satırındaki `ip_match` / `policy_context` sahanın ağını ve
   koordinatını dolaylı açabilir; panel göstermez, işveren kendi verisini dışa aktarabilir.
6. **K-38:** çapraz QR guardrail'ı işverence ne gevşetilir ne `deny`'e sıkılaştırılır.
7. **K-29:** üyelik bittikten sonra verilen onay/ret KF'ye düşmez; KF `pending_review`'u hep
   *"sonuç işverende"* görür. QR'da `left_at` yarışı *"tekrar dene"* üretir.
8. **K-31 / ADR 0028 §5:** KF'de takılı bir işlem KM'nin holding dokunuşlarını bekletir
   (tenant'lar arası yeni bağımlılık; `lock_timeout` + idle zaman aşımı sınırlar).
9. **K-36:** holding üyesinin saat dilimi v1'de değiştirilemez (koordineli değişiklik için `op_*`
   yok).
10. **K-24 (holding cümlesi):** e-posta tekilliği holding genelinde aranmaz → aynı kişiyi iki tenant
    davet edebilir; aktivasyon K-1b gereği o tenant'ın plaketinde fiziksel dokunuş ister.
11. **`scope=location` ile süre hesaplayan entegratör** holding çalışanının süresini fazla sayabilir
    (plan §2 madde 30–31; ADR 0028 sözleşme farkları).
12. **Kilit çapası** (§3): `tappa_holdingdefiner` `holding_members`'ta anlamsız bir sütunda `UPDATE`
    tutar; CHECK onu değer düzeyinde etkisiz kılar. Yetki `FOR UPDATE`'e de izin verdiği için (1. tur
    ölçtü) `prosrc` taraması `holding_members`'a yalnız `holding_peer`'in değindiğini ve oradaki tek
    kilidin `FOR SHARE` olduğunu pinler.
13. **Üyelik sürerken `tappa_app` kusurunun sahte `site_*` satırı** (§12 (d)): işverenin çalışanı ×
    eşin plaketi/lokasyonu ile sınırlı; sonuç eşin izdüşümüne ulaşır.
14. **K-29'un 60 sn'lik çıkış penceresi** (§12 madde 1): yeni çıkmış bir tenant'ın toleranslı
    kollardan yazdığı satırlar eski eşe düşer; tekilleştirmeyle sınırlı.

## Elenen seçenekler

| Seçenek | Neden elendi |
|---|---|
| **Tek tenant, iki şirket** (`companies` tablosu — planın 1.–5. sürümü, 5. tur iki mercekten ONAY) | Kullanıcı modeli değiştirdi (2026-10-10, K-1): *"taşıma yapmayacağız; o aynı holding altında ayrı iki firma, ayrı iki tenant"*. Bedelleri: KM yöneticileri hesabını ancak EM-12 ile alırdı, KM plaketleri taşınmalıydı, bir şirketin yöneticisi ötekinin kayıtlarını ve e-postalarını görürdü, politika ve saat dilimi paylaşılırdı |
| **KF'nin sahası için KM oturumundan `WithTenant(KF)`** | ADR 0016 §2'nin yasakladığı desen; KF'nin bütün RLS'i KM'nin her SQL kusuruna açılırdı |
| **`op_*`'ı müşteri yoluna açmak** | ADR 0021 *"Elenen seçenekler"*: `tappa_app`'e `op_*` EXECUTE'u müşteri tarafı her kusuru operatör yarıçapına taşır |
| **Saha bilgisini okuma anında çapraz join'le üretmek** | Okuma yolunun her sorgusu tenant sınırını aşan bir definer olurdu; K-32'nin yazma anı anlık görüntüsü ve `site_punches` izdüşümü sınırı yazma anına ve kapalı listeye toplar |
| **İki ayrı AFTER tetikleyicisi (her tenant kendi sayacını kilitler)** | 6. tur iki mercek ölçtü: ters yönlü iki holding dokunuşunda kilitlenme, birinin kaydı geri alınıyordu (K-31) |
| **Tek, kanaldan bağımsız geçiş penceresi** (*"5 dk"* ya da 60 sn her yerde) | 6.–7. tur: yalnız erteliyordu; NFC'de sahte replay, QR'da çıkarılmış tenant'a yeni yazma, okumada `static_ips` (K-29) |
| **K-1e — kapanış çıkışını KF'ye taşımak** | Kullanıcı kaldırdı (2026-10-10); her tenant'ta kayıt kaybı dahil ORTA'ların çoğunun kaynağı |
| **KF raporunda izdüşüm içi giriş/çıkış eşlemesi** | 10. tur ölçtü: sahte 9 sa / 32 sa aralıklar; KF raporu holding çalışanı için süre hesaplamaz |
| **Holding genelinde e-posta tekilliği** | Yeni bir tenant-ötesi okuma (K-24) |
| **`holding_peer`'i `tappa_app`'e açmak** | Serbest parametreli üyelik kâhini (9. tur) |

## Karar verilmedi

- **İkiden fazla tenant'lı holding'in ekranları** (plan §8). Veri modeli ve süzgeç N üyeyi taşır;
  audit *"her diğer üye"*ye yazar; UI yok.
- **Saat dilimini iki üyede birlikte değiştiren `op_*`** (§1) — v1'de yok.
- **İşverenin raporundaki *"<şirket> — <mekân>"* kovasının şirket etiketi.** Plan kaynağı
  `holding_site_locations` diye adlandırır ama o fonksiyonun dönüşü (imza tablosu) bir şirket
  etiketi taşımaz. API-1c etiketi imza tablosundaki **mevcut** dönüşlerden seçer; o güne dek kova
  yalnız **mekân adıyla** etiketlenir. `holding_site_locations`'ın (ya da herhangi bir holding
  fonksiyonunun) dönüşünü genişletmek §2'nin kuralına tabidir — **kullanıcı onayı + bu ADR'nin
  değişikliği**; tarihli bir not tek başına yetmez (1. tur düzeltmesi, güvenlik merceği).
- **KF'nin KM çalışanı için onay vermesi** (plan §8) — kapsam dışı.

## Test edilecekler (adlarıyla)

Adlar önerilir; uygulama görevi daha iyi bir ad seçerse kart düzeltmesiyle değiştirir, kapsamı
daraltamaz. *(Kaynak: plan §5 kabul çekirdeği — API-1a, API-1b, API-1c, API-2'nin holding
satırları.)*

**Şema ve katalog (API-1a)**
- `TestHoldingSchema_AppRoleHasNoVerbOnHoldingTables` — `arwd` varsayılanı kurulmuşken `tappa_app`
  `holdings`/`holding_members`'ta dört fiilde yetkisiz; R5 muafiyeti WARN'da görünür; ENABLE + FORCE.
- `TestHoldingOps_OnlyOperatorFunctionsWriteMembership`
- Audit, eylem eylem (§1 tablosu): `TestHoldingOps_CreateAuditsOnlyTheOperatorLog` ·
  `TestHoldingOps_FirstAddAuditsTheOperatorAndTheAddedTenant` ·
  `TestHoldingOps_SecondAddAuditsTheOperatorAndBothMembers` ·
  `TestHoldingOps_RemoveAuditsTheOperatorTheLeaverAndTheRemainingMember`
- `TestHoldingOps_AddRefusesADifferentTimezone` · `TestHoldingTimezone_OwnerCannotChangeAMembersTimezone`
  (çıkış penceresinde de) · `TestHoldingOps_TimezoneComparisonRacesTheOwnersUpdate` (eşzamanlı:
  `op_add_holding_member`'ın kanonik `tenants` kilidi ile sahibin saat dilimi `UPDATE`'i — iki
  sırada da sonuç ya ret ya eşit saat dilimiyle üyelik; farklı saat dilimli üyelik **asla**) ·
  `TestHoldingOps_ConcurrentAddsForOneTenantOneWins`
- `TestHoldingOps_OneMembershipAtATimeIncludingTheExitWindow` ·
  `TestHoldingOps_RejoinWaitsForTheToleranceWindow` (`left_at + 59 sn` ret, `+ 61 sn` kabul; iki
  60 sn kopyasının eşitliği) · `TestHoldingOps_LeftAtCannotBeUndone`
- `TestHoldingDefiner_RoleShape` (NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesiz; `proconfig` sabit)
- `TestHoldingDefiner_ColumnMatrix` (§3 matrisi, tablo × fiil × sütun) ·
  `TestHoldingDefiner_NeverSelectColumns` (*"asla SELECT"* listesi + `%_hash` katalog sorgusu)
- `TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate` (üyelik sütunlarında UPDATE `false`;
  `lock_anchor = true` → 23514; gövdelerde `UPDATE public.holding_members` yok)
- **`TestHoldingCatalog_TheFunctionsAndTheirExactSignatures`** (§2: ad kümesi = imza tablosu;
  argüman ve dönüş metni birebir; ad başına tek aşırı yükleme; her `holding\_%`'nin sahibi
  `tappa_holdingdefiner`; `prosecdef`/`provolatile`/`proconfig`/EXECUTE) — mutant: dönüşüne
  `static_ips` eklenmiş `holding_site_locations` → kırmızı
- `TestHoldingPeer_InvokerVolatileOwnedByDefinerExecutableOnlyByOwner` ·
  `TestHoldingPeer_ModesAreAClosedSet` (bilinmeyen kip → hata; `a = b` → `false`) ·
  `TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers` (`prosrc` taraması: `holding_members`
  geçen her gövde ya `holding_peer` ya `tappa_opdefiner` sahipli `op_%`) ·
  `TestHoldingPeer_OnlyRowLockIsForShare` (`holding_peer`'de `FOR UPDATE` / `FOR NO KEY UPDATE` /
  `FOR KEY SHARE` / `UPDATE` / `INSERT` / `DELETE` / `MERGE` yok; mutant: ters sırada `FOR UPDATE`
  → kilitlenme)
- `TestHoldingFunctions_ExecuteGrantedOnlyToApp` · `TestHoldingTriggers_ExecuteOnlyOwnerAndAssertRelid`
  (geçici tablo bağlama reddi)
- `TestRestore_FreshPodCarriesTheHoldingRole` (sıfırdan pod; `01-roles.sql` idempotent)

**Çapraz dokunuş (API-1b)** — harf, plan §5 API-1b kabulünün maddesidir
- (a) `TestHoldingTap_RecordLandsAtTheEmployerWithSiteColumns`
- (b) `TestHoldingTap_OutsiderSessionOnAPeerPlaqueIsRefusedAndCounterUnchanged` ·
  `TestHoldingTap_PeerSessionOnAnOutsiderPlaqueLeavesItsCounter` ·
  `TestHoldingFunctions_ReturnNothingForAnOutsiderOrAnOutsiderKey` ·
  `TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers` (C, H2'nin üyesi) ·
  `TestHoldingManual_PeerManagerCannotNameAnOutsiderLocation` ·
  `TestHoldingDirectory_ReturnsPeersNeverTheCaller` (çağıranın kendi tenant'ı `holding_directory`'den
  hiç dönmez; C bağlamında 0 satır — K-33)
- (c) `TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins` (`-race`, yarı/yarı)
- (d) `TestDecide_PersonDebounceSpansBothTenantsPlaques` · `TestHoldingTap_DirectionChainStaysAtTheEmployer`
- (e) `TestHoldingTap_SiteLocationExceptionsDoNotApply`
- (f) `TestDecide_CrossTenantQRWithoutSiteIPIsFlagged` (işveren QR kuralını gevşetse bile) ·
  `TestDecide_CrossTenantQRWithSiteIPIsOK` · `TestGuardrails_CrossTenantQRSitsRightAfterDebounce` ·
  `TestGuardrails_CrossTenantQRCannotBeLoosenedOrTightened`
- (g) `TestHoldingWindow_AdvancedBeforeLeftWrittenAfterIsRecorded` ·
  `TestHoldingWindow_LeftBetweenAdvanceAndGatherFlagsWithoutSiteEvidence` ·
  `TestHoldingWindow_AdvanceAfterLeftIsNotPeer` · `TestHoldingAdvance_EqualCounterIsReplayWithSiteColumns` ·
  `TestHoldingTap_LostPlaqueRefusalRecordedAtEmployerSeenAtSite` ·
  `TestHoldingAdvance_InactivePlaqueIsPeerInactiveNot403` ·
  `TestHoldingWindow_RefusalsSurviveTheLeftRace` (replay, `peer_inactive`, kayıp plaket) ·
  `TestHoldingAdvance_DecideSeesTheCurrentStatus` (kanca ile kurulan yarış) ·
  `TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter` (FK beklemesi + commit edilmemiş `op_remove`;
  `FOR SHARE` sıralaması) · `TestHoldingWindow_QRRefusalSurvivesTheLeftRace` ·
  `TestHoldingWindow_UnadvancedNFCRowAfterLeftIsRefused` · `TestHoldingWindow_QRDecisionAfterLeftIs403` ·
  `TestHoldingWindow_ManualAfterLeftIsRefused` · `TestHoldingReads_AfterLeftReturnNothing` ·
  `TestHoldingWindow_ToleranceIsAtLeastTwiceTheRequestTimeout` ·
  `TestHoldingPeer_AppCannotCallIt` (katalog satırı)
- (h) `TestHoldingActivation_OnlyOnItsOwnTenantsPlaque` ·
  `TestHoldingTap_PendingActivationBrowserWithLiveSessionTapsAsHolding`
- (i) `TestHoldingBrand_TapAndResultShowTheSiteBrand` · `TestHoldingLogo_VIESGate` ·
  `TestHoldingLogo_OutsiderOrStaleDigestIs404`
- (j) `TestHoldingCheck_ChannelRules` (lokasyonsuz plaket reddi yazılır; manuel saha yazılır;
  `site_tenant_id = tenant_id` ret; `site_tenant_id` NULL iken `site_*` dolu ret)
- (k) `TestHoldingMigration_AlterWithinTwoSeconds` (ölçüm; aşılırsa `NOT VALID` yolu)
- (l) `TestHoldingAdvance_KeepsReplayProtection` (eşit/geri `ctr` → `replay`, sayaç değişmez;
  holding dışı → `not_peer`)
- `TestDecide_TenantMismatchRespectsSameHolding` (CLAUDE.md §5'in `TestDecide_…` kuralı)

**İzdüşüm ve paneller (API-1c)**
- `TestSitePunches_CatalogCarriesNoEvidenceColumns` · `TestSitePunches_OutcomeMappingIsTotal`
  (`ignored` yazılmaz, işverenin kaydı geri alınmaz)
- `TestSitePunches_ReplayURLCollapsesAtTheSite` (N → KF'de 1, işverende N) ·
  `TestSitePunches_DifferentEmployeesReplaysStaySeparate` ·
  `TestSitePunches_ForgedCountersCollapsePerHourBucket` (200 sahte URL) ·
  `TestSitePunches_RealRefusalsAreKeptOneEach` (3 → 3) ·
  `TestSitePunches_LostPlaqueRepeatsGiveOneRowAndOneAlert` ·
  `TestSitePunches_LostPlaqueAlertCannotBeSuppressed` ·
  `TestSitePunches_BucketIsServerClockAndZoneFree` (UTC ve Europe/Malta aynı kova) ·
  `TestSitePunches_NullHolesAreDeduplicated` · `TestSitePunches_AppCannotReadSunValid` ·
  `TestSitePunches_LastSeenIgnoresDeclaredTime` (`occurred_at=2099`) ·
  `TestSitePanel_LostPlaqueAlertUsesTheServerClock` (KF kayıp plaket uyarısının zamanı
  `site_punches.created_at`'ten; `occurred_at` beyanı uyarıyı ne öne ne geriye kaydırır)
- `TestSiteReport_HoldingStaffHaveNoHoursAtTheSite` (KF→KM ve KM→KF ardışık iki vardiya, aynı gün ve
  iki güne yayılan → 0 sa + dokunuş listesi) · `TestSiteReport_ApprovedFlagFoldsToItsLastOutcome` ·
  `TestSiteReport_OwnStaffHoursUnchanged` · `TestSiteReport_EmployerManualSeparateAndExcluded`
- `TestSitePunches_DeactivatedEmployeeRefusalIsOnlyRefused` · `TestSitePanel_LostPlaqueAlertAtTheSite` ·
  `TestSitePanel_CannotReview`
- `TestEmployerReaders_NineReadersNameTheSite` (her yönde, dokuzunun her biri ayrı vaka)
- `TestLocationDelete_CountsHoldingReferences` · `TestSitePunches_OutsiderSeesNothing`

**Holding tetikleyicisi (API-2'nin holding satırları)**
- `TestHoldingTrigger_WritesBothTargetRows` (KM@KF: KM satırında KF lokasyon id/ref/ad/şirket kodu;
  KF satırında KM çalışan id/ref/kod/şirket kodu) · `TestOwnTenantTrigger_NeverTouchesSiteRows` ·
  `TestHoldingTrigger_EmployerEventExactlyOnce`
- `TestHoldingTrigger_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce` (karşı taraf NULL; KF
  atlanır, hata yok)
- `TestHoldingTrigger_MissingCounterRowsNoDeadlock` (*"var et"*siz mutant kırmızı) ·
  `TestHoldingTrigger_NameOrderIsFirst` · `TestHoldingTrigger_WrittenTablesColumnPins`

## Sonuçlar

- **CLAUDE.md** güncellendi (bu ADR ile aynı değişiklikte): §4.5 iki istisna, §5 guardrail #1'in
  eşlik şartı ve `sys:cross-tenant-qr-site-ip`, §6 tenant kapsamsız tablolar (`holdings`,
  `holding_members`), §9 holding plaketinde markanın plaketin tenant'ına ait olduğu, §3 dizin
  haritası.
- **Notlar** (her birinin sonunda *"M11 notu (2026-10-11)"*): ADR 0002 (md.6, md.7), 0004, 0006,
  0007, 0008, 0016, 0021, 0023, 0024, 0025, 0026. `open-questions.md` Y2.
- **`internal/policy/guardrails.go`** — #1'in yorumu, M11'de holding içi görünürlüğün bu ADR ile
  değişeceğini söyler (davranış değişmedi).
- **Uygulama sırası** (plan §5): API-1a (varlık, rol, `op_*`, K-36) → API-1b (çapraz dokunuş) →
  API-1c (izdüşüm, paneller) → API-2 (olaylar; holding tetikleyicisinin sayaç/olay yarısı). Her biri
  yapıcı → üçüncü göz → `tappa-security-auditor`.
- **Canlıya alma** (API-13): operatör KF+KM holding'ini kurar; holding dışı izolasyon prod'da
  örneklenir.
