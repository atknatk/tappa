# ADR 0027 — Holding: aynı holding'deki iki tenant arasında dokunuş ve görünürlük — CLAUDE.md §4.5'in ikinci bilinçli istisnası

- **Durum:** kabul edildi — **uygulama: yok (M11).** Bu ADR yazıldığında (HEAD `bcbff6d`, dal
  `m11-api`) şemada `holdings`, `holding_members`, `transactions.site_*` sütunları, `site_punches`,
  `tappa_holdingdefiner` rolü ve tek bir `holding_*` fonksiyonu **yoktur**; bir tenant'ın oturumuyla
  başka bir tenant'ın plaketine dokunmak `sys:tenant-mismatch` ile reddedilir ve hiçbir yere kayıt
  yazılmaz (aşağıda *"Bağlam"*). Uygulama görevleri: API-1a, API-1b, API-1c ve API-2'nin holding
  yarısı ([m11-entegrasyon-api.md](../plan/m11-entegrasyon-api.md) §5). **API-1a uygulandı
  (2026-10-13, migration `00040`–`00042`):** `holdings`, `holding_members`, `tappa_holdingdefiner`,
  `holding_peer`, saat dilimi kilidi ve holding'in operatör yüzeyi var — en sondaki *"API-1a uygulama
  notu"*. **API-1b uygulandı (2026-10-14, migration `00043`–`00045`; `00045` kullanıcının K-1i
  kararı — `ctr_gap`):** `transactions.site_*` + K-30
  CHECK'leri, `holding_tap_site`, `holding_advance_tag_counter`, `holding_logo`, `holding_site_gate`;
  guardrail #1'in eşlik şartı ve `sys:cross-tenant-qr-site-ip`; checkin'in holding yolu, K-35, logo
  rotasının eş biçimi ve tap/sonuç ekranında sahanın markası — en sondaki *"API-1b uygulama notu"*.
  **API-1c uygulandı (2026-10-14, migration `00046`–`00047`):** `site_punches` ve `holding_feed`'in
  izdüşüm yarısı, red tekilleştirmesi, `holding_site_locations`, işverenin dokuz okuyucusu, sahanın
  panel görünümleri (rapor listesi, kayıp plaket uyarısı, sayaç boşlukları, son görülme), eşin
  mekânında manuel kayıt ve lokasyon silme sayımı — en sondaki *"API-1c uygulama notu"*. Olaylar
  (API-2) hâlâ yok.
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
yüzeye hiçbir yoldan erişemez** (tek ve sınırlı istisna: bir üyelik bittikten sonraki 60 sn —
K-29'un çıkış penceresi — içinde toleranslı kollardan yazılan satırlar **iki yönde de** karşı tarafa
düşer — §6, §12), **eş bir tenant holding dışı bir anahtarı (plaket uid'i,
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
| **K-1i** | Holding dokunuşunda sayaç atlaması (`ctr_gap`) | **İşverene ulaşır** (2026-10-14): `holding_advance_tag_counter`'ın dönüşüne `ctr_gap integer` eklenir — yalnız `advanced`'ta, **aynı ifadede** `p_ctr − eski last_ctr − 1`, öteki sonuçlarda NULL; işverenin `base:ctr-gap-review`'ı holding dokunuşuna tek-tenant dokunuştaki anlamıyla uygulanır. Gerekçe: *"URL toplama sinyali karar veren işverene ulaşsın"* | Kapalı listenin bir dönüşü genişledi (§2, migration `00045`). Atlama **bir kez**, holding dokunuşunda yakalanır: eski `last_ctr` ilerletme ifadesinden sonra yoktur, sonraki KF dokunuşu onu yeniden görmez (§13). KF tarafındaki iz API-1c'nin (§9). **K-1i ile KM kendi satırından plaketin dokunuştan önceki mutlak `last_ctr`'ını türetebilir** (`ctr − tap:ctrGap − 1`; ölçüldü: 150, 49 → 100) — `ctr` KF'nin yeni değerini `00043`'ten beri zaten taşıyor; K-1i'nin eklediği, KM'nin iki dokunuşu arasında KF'nin ya da başka bir eşin kabul edilmiş son okumasının sayaç değeridir; replay için sömürülemez (sayaç yalnız artar, ilerletme katı `<`) (§12 madde 3) |

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
  tenant'ın aynı saati okumasını ister). **Önce holding'i serileştirir** (2. tur, iki denetçi ölçtü:
  **boş** bir holding'e farklı saat dilimli iki tenant eşzamanlı eklenince ikisi de karşılaştıracak
  üye görmüyor ve K-36 deliniyordu): `pg_advisory_xact_lock(hashtextextended('holding-membership:'
  || holding_id::text, 0))` — deponun mevcut kalıbı (`db/queries/invites.sql:403`); advisory kilit
  fonksiyonları ve `hashtextextended` PUBLIC'e açıktır, yani **yeni bir yetki gerekmez**. Kilitten
  sonraki üyelik okuması yeni bir ifadedir (READ COMMITTED, VOLATILE gövde) ve bekleyen eklemenin
  commit edilmiş üyeliğini görür. Aynı kilidi `op_remove_holding_member` da alır (audit'in *"kalan
  üye"* kümesi eşzamanlı bir eklemeyle yarışmasın). **Elenen:** `holdings` satırını `FOR NO KEY
  UPDATE` ile kilitlemek — `tappa_opdefiner`'a `holdings`'te bir sütunda yeni bir `UPDATE` yetkisi
  isterdi (*"Bağlam"*: satır kilidinin yetki şartı). **Sonra, karşılaştırmadan önce kilitler** (1.
  tur, üçüncü göz ölçtü: kilitsiz karşılaştırma ile ekleme arasına sahibin saat dilimi `UPDATE`'i
  girebiliyordu): ilgili `tenants` satırlarını **kanonik sırada** (küçük id önce) — holding'in mevcut üyelerininkini
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

**API kodu holding içinde ayırt edicidir (API-1a, 2026-10-13 — K-22'nin teknik sonucu, yapıcının
otonom kararı; API-1 denetiminin devri).** Spec'in `company` alanı (`api_code`, ör. `KF`/`KM`) bir
punch'ın işverenini ve sahasını adlandırır (ADR 0028 §4'ün anlık görüntüsü); bir holding'in iki
üyesi aynı kodu taşırsa bir tenant'ın olayları ötekine atfedilir. Kod **genel olarak tekil değildir**
(00038 — tenant'lar arası tekillik yazmayı tenant-ötesi bir eyleme çevirirdi) ama **holding içinde
iki yönde de** ayırt edicidir: `op_add_holding_member`, eklenen tenant'ın kodunu holding'in canlı
**ya da 60 sn'lik çıkış penceresindeki** bir üyesi taşıyorsa eklemez; `op_enable_tenant_api` (00042
ile değiştirildi) bir üyeye, canlı ya da penceredeki bir eşinin kodunu yazmaz. NULL kodlar çakışmaz
(kodsuz üyenin API'si yoktur). İki yön **aynı holding danışma kilidi** altındadır (ekleme ve çıkarma
gibi), ardından satırlar kanonik sırada kilitlenir (yazılan tenant `FOR NO KEY UPDATE`, eşleri `FOR
SHARE`); `op_enable_tenant_api` tenant'ın holding'ini kilitten önce okur ve tenant o arada bir
holding'e katılmışsa **hiçbir şey yazmaz** (aynı `void`; tenant satır kilidi o pencerenin tek kapısı,
kilit sırası `op_add_holding_member`'ınkiyle aynı kalır). Ret, ADR 0021 §2 v 7 gereği **sessizdir**:
cevap yazıldığı zamankiyle aynı `void`, operatör satırı yazılır, tenant satırı ve kod yazılmaz;
sonuç iki aşamalı okumayla görülür. Testler: `TestHoldingAPICode_AddRefusesAPeersCode`,
`TestHoldingAPICode_EnableRefusesAPeersCode`, `TestHoldingAPICode_EnableAndAddRaceUnderOneLock`
(ayrıntı ve mutantlar *"API-1a uygulama notu"*).

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
   parametre olarak **alınmaz**. Bağlam yoksa (NULL) cevap **0 satırdır**.¹
4. **Girdi yalnız sunucunun çözdüğü anahtardır** (plaket uid'i, sayaç değeri, logo özeti, imleç,
   `updatedSince`); istemcinin seçtiği bir tenant kimliği hiçbir fonksiyonun parametresi değildir.
5. **Süzgeç (K-33, normatif):** *anahtar satırının tenant'ı ∈ eşler(GUC tenant'ı) ∧ GUC tenant'ı bir
   holding'in üyesi* — ikisi **aynı** holding'de (`holding_id` eşitliği). Eş değilse **0 satır**,¹
   bilinmeyen anahtarla **aynı** cevap (kehanet yok — ADR 0026'nın *"aynı ekran ve durum kodu"*
   ilkesi).
6. **Gövdede RLS yoktur** (sahip BYPASSRLS) → açık süzgeç ve her ifadedeki açık tenant filtresi
   **tek bariyerdir** (ADR 0021 §3.2'nin aynısı).
7. **Hata yutan `EXCEPTION` bloğu yok** (savepoint geri alımı kilidi bırakır ve yazma sessizce
   düşerdi — ADR 0028 §4); eşlik bittiğinde bir dalı atlamak açık bir `IF`'tir, hata yutma değil.
8. **Ağ, uzun iş, uç nokta adresi okuma yok.**

¹ **Adlandırılmış tek istisna — `holding_advance_tag_counter` (API-1b, 2026-10-14; denetimde onay
gerektirmeyen netleştirme sayıldı).** İmza `not_peer`'i bir **sonuç** olarak adlandırır, bu yüzden
fonksiyon **her zaman tek satır** döner: bağlamsız çağrının, eş olmayanın ve bilinmeyen uid'in cevabı
aynı `('not_peer', NULL, NULL)` satırıdır. Madde 5'in amacı (kehanet yok) korunur — üç durum ayırt
edilemez; değişen yalnız *"0 satır"*ın yerine *"tek, aynı satır"*dır.

**Tek yüklem — `holding_peer(a, b, mod)` (K-33).** *"İki tenant aynı holding'de eş mi"* sorusunun
**tek** cevabı budur; bütün `holding_*` fonksiyonları **ve iki tetikleyici** (BEFORE INSERT üyelik
kapısı, AFTER holding tetikleyicisi) onu çağırır. **Kural (1. tur düzeltmesi, 2026-10-11, üçüncü
göz): `holding_members`'a gövdesinde değinen TEK fonksiyon `holding_peer`'dir** (yazarlar olan
`tappa_opdefiner` sahipli `op_*`'lar hariç); BEFORE kapısının kilidi ve K-36 kilidinin üyelik sorusu
da bu yüzden `holding_peer`'in kipleridir. Kural **yapısal değildir, bir taramayla tutulur** (aşağıda):
`tappa_holdingdefiner`'ın `holding_members`'ta `SELECT` yetkisi olduğu için bir gövde tabloyu
okuyabilir; satır içi eşlik kontrolü yazmak taramayı kırmızıya çevirir. Tarama sözdizimseldir, kanıt
değildir — kapsadığı biçimler aşağıda sayılıdır, kalanlar kod incelemesinin konusudur. `mod` **kapalı** bir kümedir (bilinmeyen kip →
hata):

| Kip | Soru | Saat | Çağıran |
|---|---|---|---|
| `strict` | `a` ve `b` aynı holding'de, iki üyelik de `left_at IS NULL OR left_at > clock_timestamp()` | `clock_timestamp()` | çağrılabilir `holding_*` fonksiyonları (`holding_advance_tag_counter`'da tek CTE'de **bir kez**) |
| `strict_stmt` | aynı soru, `statement_timestamp()` ile | ifade saati | BEFORE kapısının katı kolları (QR, manuel, ilerletilmemiş NFC); AFTER tetikleyicisinin onay kolu |
| `tolerant_stmt` | aynı holding, iki üyelik `left_at IS NULL OR left_at + 60 sn > statement_timestamp()` **ve iki üyelik zamanda örtüşür:** `ma.joined_at < coalesce(mb.left_at, 'infinity') AND mb.joined_at < coalesce(ma.left_at, 'infinity')` | ifade saati | **yalnız** BEFORE kapısının K-29 (i)/(ii) kolları (§6) |
| `lock` | `tolerant_stmt`'in şartını (aynı holding + pencere + **örtüşme**) sağlayan aday üyelik satır çiftlerini kanonik sırada (küçük tenant id önce) **`FOR SHARE`** kilitler; karar vermez | — | **yalnız** BEFORE kapısı, karardan önce (§6) |
| `is_member` | `a`'nın (`b` NULL) `left_at IS NULL OR left_at + 60 sn > clock_timestamp()` olan bir üyeliği var mı | `clock_timestamp()` | K-36 saat dilimi kilidi (§1) · *(API-8, 2026-10-15, `00055`)* `holding_directory` ve `holding_site_locations`'ın **üyelik kapısı**: hayırsa eş kümesi hiç hesaplanmaz; evetse karar yine `strict`'tir (kapı bir şey karara bağlamaz — toleranslı kip katının üst kümesidir, eş gizleyemez) |

**Örtüşme şartı neden (2. tur, güvenlik merceği ölçtü):** yalnız *"aynı `holding_id` + pencere"*
soran toleranslı kip, holding'den **çıkmış** A'nın pencerede kalan eski satırını, A çıktıktan
**sonra** katılmış B'nin satırıyla eşleyip hiç birlikte üye olmamış iki tenant'ı eş sayıyordu. Katı
kiplerde iki üyelik de *şu an* canlı olduğu için örtüşme kendiliğinden sağlanır.
`a = b` → `false` (bir tenant kendisinin eşi değildir; kendi verisi RLS'li yoldan gelir — rehber dahil,
ADR 0028 §2). 60 sn'nin tek tanımı `holding_peer`'dedir (`op_add_holding_member`'ın kopyası §1).
**Maliyet — eş kümesi bir kez (2. tur, güvenlik merceği ölçtü: 5,54 sn → 42 ms).** VOLATILE bir
fonksiyon satır başına yeniden çağrılır; bu yüzden çok satırlı fonksiyonlar (`holding_directory`,
`holding_site_locations`) eş kümesini **bir kez** hesaplar — `tenants` üzerinde `holding_peer(GUC
tenant'ı, t.id, 'strict')` ile, materyalize bir CTE'de — ve veri satırlarını bu kümeyle birleştirir;
`holding_peer`'i veri satırı başına **çağırmaz**. *(API-8, 2026-10-15 — T124'ün yarısı)* Bu tarama, önünde tek bir
`holding_peer(GUC tenant'ı, NULL, 'is_member')` kapısıyla koşar (bir InitPlan): holding'siz işletme `tenants`'ı hiç
taramaz (21 bin tenant'ta ~0,7 sn → ~1 ms); bir üye taramanın bedelini hâlâ öder (~1 sn — §13 madde 21).
`holding_peer` **VOLATILE**'dir (`lock` kipi satır kilidi alır; PostgreSQL kilitli `SELECT`'i volatil
olmayan fonksiyonda reddeder — *"Bağlam"*), `holding_members` üzerindeki **tek satır kilidi
`FOR SHARE`'dir** — gövdesinde `FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR KEY SHARE`, `UPDATE`,
`INSERT`, `DELETE`, `MERGE` **yoktur** (1. tur, güvenlik merceği ölçtü: `lock_anchor`'ın `UPDATE`
yetkisi `FOR UPDATE`'e de izin veriyor ve ters sırada alınan `FOR UPDATE` kilitlenme üretiyor — §3).
**Gövde taraması (normatif; 2. tur düzeltmesi — güvenlik merceği `prosrc` üzerindeki sözcük
düzeyindeki taramanın dört atlatmasını ölçtü):**
1. Metin `prosrc` değil **`pg_get_functiondef(oid)`**'dir, **yorumları ayıklanmış** olarak (`--` ve
   iç içe `/* */`), büyük/küçük harf duyarsız; her şemadaki her fonksiyon dolaşılır.
2. Bu metinde `holding_members` geçen her fonksiyon ya `public.holding_peer`'dir ya da sahibi
   `tappa_opdefiner` olan bir `op_%`'dir.
3. SQL-standart gövdeli (`BEGIN ATOMIC`, `prosqlbody` dolu) her fonksiyon için ayrıca **`pg_depend`
   kenarları** denetlenir: `holding_members`'a bağımlılık kaydı yalnız aynı iki sınıfta olabilir
   (PL/pgSQL gövdesi bağımlılık kaydetmez — metin taraması o yüzden gerekir).
4. `tappa_holdingdefiner` sahipli **her** gövdede **`EXECUTE`** (dinamik SQL — §2 sözleşmesinin 1.
   maddesi bununla pinlenir) ve **`U&"`** (Unicode kaçışlı tanımlayıcı) **yoktur**.
5. `holding_peer`'in metninde yukarıdaki yasak deyimlerin (`FOR UPDATE`, `FOR NO KEY UPDATE`, `FOR KEY
   SHARE`, `UPDATE`, `INSERT`, `DELETE`, `MERGE`) hiçbiri geçmez.

Tarama bir görünümün ya da başka bir fonksiyonun arkasını görmez; `tappa_holdingdefiner`'ın üyelik
okuyabildiği tek nesnenin `holding_members` olması §3'ün matrisiyle tutulur.
`holding_peer` **SECURITY INVOKER**, sahibi `tappa_holdingdefiner`, `EXECUTE` **yalnız sahipte**:
yalnız definer gövdelerinin içinden (orada `current_user` = sahip) çağrılabilir. `tappa_app`'e
asla — aksi hâlde serbest parametreli bir **üyelik kâhini** olurdu (9. tur ölçtü). 10. tur ayrıca
ölçtü: INVOKER olduğu için yanlışlıkla açılsa da `tappa_app` `holding_members`'ı okuyamaz. ADR
0021'in `prosecdef` sahip testi INVOKER'ı kapsamadığı için katalogda **ayrı bir satırla** pinlenir.

**Kapalı fonksiyon listesi — holding istisnasının tamamı.** Bu listede olmayan hiçbir kod yolu
tenant bağlamı olmadan bir tenant satırına dokunamaz (`op_*` hariç — ADR 0021). 🔴 **Listeye fonksiyon
eklemek, bir fonksiyonun dönüş sütunlarını değiştirmek ya da genişletmek, `tappa_holdingdefiner`'ın
sütun matrisini (§3) genişletmek, ya da `holding_peer`'e kip eklemek veya bir kipin süzgecini ya da
zaman kuralını gevşetmek KULLANICI ONAYI + bu ADR'nin değişikliği ister** (tarihli not + iki
merceğin denetimi); argüman değişikliği yalnız bu ADR'nin değişikliğidir. *"Zaten N tane var"*
gerekçe değildir (ADR 0021'in md.7 kuralı). **Neden kullanıcı onayı (1. tur, güvenlik merceği,
ölçüldü):** dönüşü `static_ips`/GPS ile genişletilmiş bir `holding_site_locations` KM'ye KF'nin ağını
ve koordinatını döndürdü (K-39 ihlali) ve bu ADR'nin o güne kadarki hiçbir testi kırmızı olmadı — bir
dönüş sütunu, tenant sınırının ne kadar açıldığının kendisidir. Kural aşağıdaki **imza tablosu** ve
**katalog testiyle** mekanik olarak sabitlenir.

| Fonksiyon | Ne döner / ne yapar | Çağrı yeri | EXECUTE |
|---|---|---|---|
| `holding_tap_site(p_uid)` | Eş tenant'ın plaketi: `site_tenant_id`, plaketin durumu; varsa `location_id`, lokasyon adı, `external_ref`, `static_ips`, GPS, vardiya, `overnight`; saha tenant'ının saat dilimi ve `api_code`'u; marka meta (accent, logo boyutu ve logo `alt`'ı olan işletme adı — **logo özeti yalnız VIES-doğrulanmış** tenant için). `static_ips`/GPS yalnız sunucu içi karar girdisidir (K-39) | `GET /t` (sayfa: KF mekân adı + KF markası) · `checkin.gather` (kilitli işlem içinde — `SameHolding`'in kaynağı, §5) | `tappa_app` |
| `holding_advance_tag_counter(p_uid, p_ctr)` | **Tek ifade** (`db/queries/tags.sql:33-46`'nın CTE + `FOR UPDATE` kalıbı): eş şartı `prev`'de **ve** `UPDATE … WHERE`'de, **katı** üyelik (`left_at > clock_timestamp()`), üyelik **tek CTE'de bir kez** hesaplanır (`clock_timestamp()` iki kez değerlendirilmez). Dönüş `(sonuç, plaketin güncel durumu, ctr_gap)`; sonuç dört değerden biri: eş değil → `not_peer` · eş ama plaket `active` değil → `peer_inactive` · eş, aktif, güncelleme boş → `replay` · güncelleme dolu → `advanced` (6.–10. turda ölçüldü: 20 eşzamanlı çağrıda tam 1 `advanced`, 19 `replay`). `ctr_gap` (K-1i, 2026-10-14) yalnız `advanced`'ta dolu: aynı ifadede `p_ctr − eski last_ctr − 1` — tek-tenant ifadesinin `t.last_ctr − prev.old_ctr − 1`'inin aynısı; öteki sonuçlarda NULL | **`checkin.advance`** — `sun.Verify`'a dokunulmaz (o yalnız aktivasyon yoludur) | `tappa_app` |
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
| `holding_advance_tag_counter` | `p_uid character, p_ctr integer` | `TABLE(outcome text, tag_status text, ctr_gap integer)` — `outcome ∈ {advanced, not_peer, replay, peer_inactive}`; `ctr_gap` yalnız `advanced`'ta dolu (K-1i, migration `00045`) |
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
`internal/db/operatortenants_test.go:388`, `TestOperator00029_TheFunctionsAndTheirExactSignatures`).
Her madde **bütün şemaları** `nspname.proname` ile dolaşır (yalnız `public` değil — 2. tur):
(i) sahibi `tappa_holdingdefiner` olan fonksiyonların **nitelikli ad kümesi** imza tablosuna eşittir
(hepsi `public.` altında) — eksik ya da fazla bir ad kırmızı; (ii) her birinin argüman ve dönüş metni
tablodakiyle **birebir**; (iii) nitelikli ad başına **tek aşırı yükleme** (ikinci bir imza ikinci bir
kapıdır); (iv) **hangi şemada olursa olsun** adı `holding\_%` olan **her** fonksiyonun sahibi
`tappa_holdingdefiner` (ters yön — sahipliği unutulmuş bir `holding_*` superuser sahipli bir definer
olurdu); (v) `prosecdef` `holding_peer` dışında `true`, `holding_peer` için `false` ve `provolatile =
'v'`; `proconfig` = `search_path=pg_catalog, pg_temp` (tetikleyicilerde ayrıca `lock_timeout`); (vi)
`EXECUTE` ACL'i **tam eşitlikle**: `aclexplode(coalesce(proacl, acldefault('f', proowner)))`'tan
`EXECUTE` alan rollerin kümesi çağrılabilir fonksiyonlarda **tam olarak** {sahip, `tappa_app`},
`holding_peer` ve tetikleyicilerde **tam olarak** {sahip}'tir — PUBLIC ya da başka herhangi bir rol
(ör. `tappa_operator`, `tappa_resolver`) kırmızı. *"Şu rollerde `false`"* biçimindeki ilk hâl yetmiyordu
(2. tur, güvenlik merceği ölçtü: `tappa_operator`'a `EXECUTE` verilince test yeşil kalıyor ve operatör
audit'siz KF'nin `static_ips`'ini okuyordu); `coalesce` şarttır, çünkü `NULL` `proacl` varsayılan ACL
(PUBLIC `EXECUTE`) demektir ve `aclexplode(NULL)` boş döner. Dönüşü genişletilmiş bir
`holding_site_locations` (1. turun mutantı) bu testte kırmızıdır.

**Kanarya testi — `TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks`** (2. tur, güvenlik merceği
ölçtü: imza testi dönüşün **içeriğini** pinlemiyor — `location_name`'e `static_ips`/GPS birleştiren bir
gövde yeşil kalıyordu). Eş tenant'ın lokasyonuna ayırt edici bir kanarya tohumlanır: bir TEST-NET
adresi (ör. `198.51.100.77/32`) ve başka hiçbir yerde geçmeyen bir GPS çifti. Sonra her **çağrılabilir**
`holding_*` (`holding_tap_site`, `holding_advance_tag_counter`, `holding_logo`,
`holding_site_locations`, `holding_directory`) eş bağlamında çağrılır ve **her çıktı hücresinin**
metin biçiminde kanaryanın (adresin ve GPS'in ondalık yazımlarının) **geçmediği** doğrulanır —
**tek istisna** `holding_tap_site`'ın `static_ips`, `gps_lat`, `gps_lng` sütunlarıdır (K-39: yalnız
sunucu içi karar girdisi). Holding tetikleyicisinin aynı tohumla yazdığı dört tablonun (`site_punches`,
`punch_events`, `webhook_deliveries`, `feed_counters`) satırları da aynı denetimden geçer. Mutant:
`location_name || static_ips` döndüren gövde → kırmızı.

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
| NFC ilerletme (`holding_advance_tag_counter`) | **katı** (`left_at > clock_timestamp()`) | `left_at`'ten sonra → `not_peer` → 403, kayıt yok, sayaç ilerlemez. `advanced` atlanan okumaları da (`ctr_gap`) aynı ifadede döndürür — işverenin `base:ctr-gap-review`'ı onu okur (K-1i) |
| NFC yazma — ilerletilmiş dokunuş | **60 sn tolerans**, kol (i): `sun_valid IS TRUE` ∧ saha plaketinin `last_ctr ≥ NEW.ctr` | ilerletme `left_at`'ten önce, yazma sonra → holding kaydı **yazılır** |
| Red satırı (NFC **ve** QR) | **60 sn tolerans**, kol (ii): `channel IN ('nfc','qr')` ∧ `verdict='reject'` ∧ (`sun_valid IS NOT TRUE` ∨ saha `last_ctr ≥ NEW.ctr`) | replay, `peer_inactive`, kayıp plaket ve QR reddi (ör. deaktive çalışanın güvenlik uyarılı denemesi) karar ile yazma arasında `left_at` geçse de **kaybolmaz** (10.–11. tur ölçtü). Red satırı mesai doğuramaz: yön NULL'a zorlanır, yalnız `flag` onaylanır. Son şart `sun_valid=true` taşıyıp ileri `ctr` yazan bir ham yazıcıyı sınırlar |
| İlerletilmemiş NFC satırı (`last_ctr < ctr`, `sun_valid` yanlış, `ok`/`flag`) | tolerans yok | `left_at`'ten sonra INSERT'te **reddedilir** |
| QR karar ve yazma | **katı** | **adlandırılmış yarış:** katı karar ile INSERT arasında `left_at` geçerse INSERT reddedilir → *"tekrar dene"*; tekrarda karar `not_peer` → kayıt yok (QR'da sayaç yok, harcanan bir şey yok) |
| Manuel kayıt | **katı** | `left_at`'ten sonra saha için manuel kayıt reddedilir |
| Onaylar (`transaction_reviews`) | **katı** (`statement_timestamp()`) | onay işverende **her zaman** yazılır (işverenin kendi kaydı); eşlik bitmişse KF tarafı (izdüşüm, saha olayı) atlanır — §9 bedeli |
| Okumalar (`holding_tap_site`, `holding_logo`, rehber, lokasyon listesi) | **katı** | `left_at`'ten sonra 0 satır |

**Toleransın uygulandığı yer veritabanıdır.** BEFORE INSERT kapısı 60 sn toleransı **yalnız**
yukarıdaki iki NFC/red koluna uygular (`holding_peer`'in `tolerant_stmt` kipi); geri kalan her şey
katıdır. Holding tetikleyicisinin `transactions` kolu eşliği **yeniden sormaz** (aşağıda) — kipi
yoktur. `sun_valid` kodda ilerletmenin başarısıdır (`checkin.go:966-970`).

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
`FOR SHARE`'in yetki şartı §3 *"kilit çapası"*. **Kilit beklemesi sınırlıdır** (kapının
`lock_timeout = 2s`'i): bir `op_remove` üyelik satırını 2 sn'den uzun tutarsa INSERT 55P03 ile düşer.
Checkin site satırını bu yüzden savepoint içinde yazar ve **yalnız** kapının 55P03'ünde aynı istekte
**bir kez** yeniden dener (API-1b denetimi, güvenlik DÜŞÜK-1): çıkarma o arada commit edildiyse
toleranslı kol ilerletilmiş NFC satırını yazar, katı satır 23514 ile reddedilir (*"tekrar dokunun"*);
ikinci bekleme de dolarsa hata yukarı çıkar — §13'ün adlandırılmış bedeli.

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
  tur: iki argümanlı biçim UTC ve Malta oturumlarında iki kova üretiyordu — **API-1c düzeltmesi,
  ölçüldü:** saat kovası için tam saat farklı bir bölge (Malta) iki biçimi ayıramaz; ayrım yarım saat
  farklı bir bölgede görünür (Asia/Kolkata: 13:30+05:30 ile 14:00+05:30) — kural aynı, testi üç
  bölgeyle kuruldu). `UNIQUE NULLS NOT DISTINCT
  (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour) WHERE sun_valid IS NOT TRUE AND
  outcome LIKE 'refused%'` — NULL `sun_valid`/`employee_id` delikleri kapalı; **`outcome` anahtarda**
  (sahte bir `refused` kayıp plaket uyarısını bastıramaz);
- iki ekleme de **hedefli** `ON CONFLICT … DO NOTHING` (§3): işverende her red **yine yazılır**
  (CLAUDE.md §4.6), KF'ye (ve kayıp plaket uyarısına) en çok bir kez. 11. tur ölçtü: 200 sahte URL →
  1 satır, gerçek 3 → 3, işverende 240/240; eşliği bitmiş tenant'ın 60 sn penceresinde KF'ye düşen
  satır tekilleştirmeyle sınırlı (2 200 red → 44).

**`tappa_app` erişimi:** `site_punches`'ta **sütun listesiyle** `SELECT`, **`sun_valid` hariç** — *sütun
olarak*; değeri bir `refused` satırın `ctr_gap`'inden ve `ctr`'ından türetilebilir (§13 madde 22)
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
- **`ctr_gap`'in kaynağı (K-1i, 2026-10-14 — eski metnin *"KF bunu izdüşümde görür"*ü kaynağı
  adlandırmıyordu).** Atlama yalnız ilerletme ifadesinin **içinde** hesaplanabilir: eski `last_ctr`'ı
  ifade kendisi üzerine yazar, sonra hiçbir okuma onu bulamaz. `holding_advance_tag_counter` onu
  `ctr_gap` olarak döndürür ve karar onu `tap:ctrGap` olarak satırın `policy_context`'ine dondurur;
  izdüşüm KF'nin `ctr_gap`'ini **oradan** — tetikleyicinin `NEW`'inden — alır. Bu bir **devirdi**
  (API-1c) ve kapandı: K-34'ün `policy_context` SELECT pini bozulmadı (tanımlayıcı sütunu okuyamaz,
  `TestSitePunches_CounterGapComesFromTheEmployersRow`) ve gövde bağlamdan yalnız bu anahtarı okur
  (`TestHoldingFeed_ReadsOnlyTheCounterGapOfThePolicyContext`).

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
**K-39 (API-1c'de uygulandı, iki denetim turundan sonra):** işverenin panelinde eş mekân satırı (`site_*`) için gösterilmeyenler: fişte ve onay kuyruğunda güven puanı, IP/GPS
işaretleri ve **kanıt anlatan** bir kuralın notu (kapalı küme, sid'e göre — `base:`/`own:` × `ip-or-gps-ok`,
`gps-only-allow`, `no-evidence-review`, `qr-requires-ip`, `gps-conflict-review`, ve `sys:cross-tenant-qr-site-ip`;
yerine *"Decided by your rules at a partner venue — evidence is not shown."*); anomali ekranında hiçbir kanıt
sayısı (judged, *GPS alone*, *outside the venue*, mekân satırının iki rakamı) ve kural dağılımında kuralın adı
(eş satırları sorguya girmez; sayısı toplam kartından gelen tek, her zaman **sonda** duran *"partner venue"* kovası). Okuyucuların envanteri
`TestEvidenceReaders_EveryOneIsClassified`'tadır. Kalan dolaylı açılma §13 madde 5.

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
   istisna:** bir üyelik bittikten sonraki 60 sn boyunca K-29'un iki toleranslı kolundan (ilerletmesi
   `left_at`'ten önce yapılmış NFC dokunuşu; replay, `peer_inactive`, kayıp plaket ve QR **redleri**)
   yazılan satırlar **iki yönde de** karşı tarafa düşer (2. tur düzeltmesi, güvenlik merceği): **çıkan**
   tenant'ın çalışanlarının kalan üyenin plaketlerindeki satırları kalanın izdüşümüne, akışına ve
   webhook'una; **kalan** üyenin çalışanlarının çıkanın plaketlerindeki satırları **çıkanın**
   izdüşümüne, akışına ve webhook'una. Red tekilleştirmesiyle sınırlı (11. tur: 2 200 red → 44); kip
   örtüşen üyelikler ister (§2), yani pencere, çıkan tenant'ın **çıktıktan sonra** katılan bir üyeyle
   eşleşmesini açmaz. Yeni
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
   bağlamı, e-posta, `verdict`, `matched_sid` ve (`tappa_app` için) `sun_valid` **sütun olarak asla**
   (katalog pinleri; `sun_valid`'in değeri başka sütunlardan türetilebilir — §13 madde 22). KM, KF'nin `static_ips`'ini ve GPS'ini görmez (K-39); KM satırının `ip_match`'i ve
   `policy_context`'i KF'nin ağını/koordinatını **dolaylı** açabilir — adlandırılmış bedel (§13).
   **K-1i (2026-10-14) ile KM kendi satırından plaketin dokunuştan önceki mutlak `last_ctr`'ını türetebilir** (`ctr − tap:ctrGap − 1`; ölçüldü: 150, 49 → 100) — `ctr` KF'nin yeni değerini `00043`'ten beri zaten taşıyor; K-1i'nin eklediği, KM'nin iki dokunuşu arasında KF'nin ya da başka bir eşin kabul edilmiş son okumasının sayaç değeridir; replay için sömürülemez (sayaç yalnız artar, ilerletme katı `<`).
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
   koordinatını dolaylı açabilir. **Panel göstermez** — işverenin panelinde eş mekân satırı (`site_*`) için gösterilmeyenler: fişte ve onay kuyruğunda güven puanı, IP/GPS
   işaretleri ve **kanıt anlatan** bir kuralın notu (kapalı küme, sid'e göre — `base:`/`own:` × `ip-or-gps-ok`,
   `gps-only-allow`, `no-evidence-review`, `qr-requires-ip`, `gps-conflict-review`, ve `sys:cross-tenant-qr-site-ip`;
   yerine *"Decided by your rules at a partner venue — evidence is not shown."*); anomali ekranında hiçbir kanıt
   sayısı (judged, *GPS alone*, *outside the venue*, mekân satırının iki rakamı) ve kural dağılımında kuralın adı
   (eş satırları sorguya girmez; tek, her zaman sonda duran *"partner venue"* kovası) (§9). **Gösterdikleri — adlandırılmış artık:** (a) satırın **verdict
   kaşesi** (ok / flag / reject) ve bekleyen onay sayısı — bir kuralın sonucu, kanıtı ima edebilir
   (*flag* çoğu zaman kanıt yetmedi demektir); (b) **`NoteIsTenants` etiketi** (*"Your own policy
   rule"*) — tenant'ın tek bir kuralı GPS-only'yi incelemeye çeviriyorsa, eş mekândaki bir `flag`'de bu
   etiket *IP hayır, GPS evet* demek olabilir (denetim D-2; not değiştirmesi bunu kapatmaz: etiket
   kimin kuralı olduğunu söylemeye devam eder); (c) kanıt kümesi dışındaki kuralların notları
   (kayıp plaket, imza, deaktive, tekrar, sayaç boşluğu, farklı şube, yön notu) ve manuel kaydın
   yöneticinin yazdığı notu — **IP/GPS kanıtını cümleye dökmezler**, ama hangi kuralın eşleştiği
   kendi başına bir şey söyler, bkz. (e); (d) tenant'ın kuralı serbest metin değildir (sevk
   edilmiş bir ifadenin gerekçesiyle kopyası, sid'i `own:<aynı ad>`), bu yüzden küme kopyaları da
   yakalar — M9-07'nin ham JSON editörü gelirse bu varsayım yeniden ölçülür; (e) **kural önceliği**
   (üçüncü denetim, DÜŞÜK; dördüncü turda düzeltildi): değerlendirici "ilk eşleşen kazanır" diye
   çalışmaz (o yalnız guardrail'ların kuralıdır). Baseline ve tenant ifadeleri birlikte tartılır:
   önce **özgüllük** (`location/<id>` > `location/*` > `*`), eşitse **kısıtlayıcılık** (deny > ignore >
   review > allow), tam eşitlikte **belge sırası** (baseline önce, ifadeler belgedeki sırayla) —
   `internal/policy/evaluate.go` başlığı. Bu yüzden eş satırında görünen kanıt dışı bir kural, kendisini
   yenebilecek kanıt kurallarının eşleşmediğini dolaylı söyler. Ölçüldü (tam baseline, varsayılan
   bağlam): (1) **review etkili** kanıt dışı bir kural — `base:ctr-gap-review` — `base:no-evidence-review`
   ile aynı özgüllükte ve aynı etkidedir, belge sırasında ondan sonra gelir; IP ve GPS ikisi de yokken
   `no-evidence-review` kazanır, biri varken `ctr-gap-review` kazanır. Yani bir eş satırında
   sayaç boşluğu notu *"IP ya da GPS eşleşti"* demektir. (2) **allow etkili** `base:cross-location-note`,
   tam baseline'da hiç kazanmaz (IP varken `ip-or-gps-ok`, yalnız GPS varken `gps-only-allow` belge
   sırasında öndedir; ikisi de yokken `no-evidence-review` kısıtlayıcılıkla kazanır). Yalnız bir kanıt
   kuralı kapatılmışsa kazanır — T125'in şekli, ve o zaman *"yalnız GPS eşleşti"* der. Not metni kanıt
   anlatmaz; artık öncelik sırasındadır. Kapatmak not
   değiştirmesini kanıt dışı kurallara da yaymak olurdu (ikinci denetimin ORTA-B'si tam onu geri
   aldı: kayıp plaket, sayaç boşluğu, farklı şube notu işverenin bilmesi gerekendir) — bu yüzden
   adlandırılmış artık, kod değişmedi. İşveren kendi verisini dışa aktarabilir.
6. **K-38:** çapraz QR guardrail'ı işverence ne gevşetilir ne `deny`'e sıkılaştırılır.
7. **K-29:** üyeliğin **commit edilmiş** bitişinden sonra verilen onay/ret KF'ye düşmez (60 sn'lik
   çıkış penceresinde de — onay katıdır; `TestHoldingFeed_ApprovalInsideTheExitWindowIsNotProjected`);
   commit edilmemiş bir `op_remove` ile kesişen onay ise düşer (onay kolu `lock` almaz — MVCC'de
   *"önce onay, sonra çıkarma"*). KF `pending_review`'u hep *"sonuç işverende"* görür. QR'da `left_at` yarışı *"tekrar dene"* üretir.
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
14. **K-29'un 60 sn'lik çıkış penceresi** (§12 madde 1): bir üyelik bittikten sonraki 60 sn içinde
    toleranslı kollardan yazılan satırlar **iki yönde de** (çıkandan kalana, kalandan çıkana) karşı
    tarafın izdüşümüne, akışına ve webhook'una düşer; tekilleştirmeyle ve örtüşme şartıyla sınırlı.
15. **K-1i:** holding dokunuşunun sayaç atlaması **bir kez**, o dokunuşu karara bağlayan işverende
    yakalanır. Plaketin kendi işletmesi (KF) sonraki tek-tenant dokunuşunda o atlamayı yeniden görmez
    (eski `last_ctr` yoktur — ölçüldü: KF@100, KM 150 → KM'de `base:ctr-gap-review` `flag`, `tap:ctrGap`
    49; ardından KF 151 → `ok`, `tap:ctrGap` 0). KF'nin görünürlüğü API-1c'nin izdüşümündedir (§9).
16. **Kapının kilit beklemesi (API-1b denetimi, güvenlik DÜŞÜK-1 ve D-2):** kapı üyelik satırlarını
    `FOR SHARE` ile en çok 2 sn bekler; checkin site satırını bir kez yeniden yazar (§6). Kalan yol:
    bekleme **iki kez** (> 4 sn) dolarsa INSERT 55P03 ile düşer — ilerletilmiş bir NFC dokunuşunda
    sayaç ilerlemiş, kayıt yazılmamış olur (kişinin tekrarı `not_peer`, 403). **Tetikleyiciler:**
    - **Üyelik satırının kilidi** > 4 sn tutulursa. `op_remove_holding_member` bir UPDATE ve en çok
      üç INSERT'tür (iki `audit_log`, bir `operator_audit_log`); bu INSERT'ler satır kilitliyken
      kendileri bekleyebilir (ör. o tablolarda bir migration'ın DDL'i arkasında). Yeniden deneme
      2–4 sn arası tutmaları kurtarır; ilk 2 sn içinde commit edilen bir çıkarma onu hiç görmez.
      Ölçüldü: `TestHoldingWindow_GateLockHeldThroughBothWaitsIsTheNamedCost`.
    - **`holding_members` üzerinde bir tablo kilidi** — o tabloya dokunan bir migration,
      `VACUUM FULL`/`CLUSTER`, elle `LOCK`, kuyrukta bekleyen bir ACCESS EXCLUSIVE isteği. Ölçüldü
      (atılabilir kap): ham bir site INSERT'i (işlemi `holding_members`'ı önceden okumamış) tablo
      kilitliyken 2,01 sn'de 55P03 alır. **Tap yolu bu beklemeye düşmez:** `gather` aynı işlemde
      tabloyu okumuştur ve tabloda zaten kilit tutan bir arka uç kuyruktaki ACCESS EXCLUSIVE'in
      önüne geçer (ölçüldü: dokunuş 404 ms'de yazıldı, kilit ardından verildi). İşlemden **önce**
      alınmış bir kilit ise `gather`'ı bekletir — orada `lock_timeout` yoktur (ölçüldü: 5 sn
      tutulan kilit, dokunuş 5,05 sn'de yazıldı); isteğin zaman aşımını aşan bir tutma
      ilerletmeden sonraki kaydı düşürür — ama **yalnız kilit ilerletmeyi engellemiyorsa**. Ölçüldü
      (API-1c dördüncü tur, 7 sn bağlamlı `svc.Record`, üçü de *context deadline exceeded*): KF'nin
      `tags` satırı ilerletmeden **önce** `FOR UPDATE` tutulursa ilerletmenin kendisi bekler —
      `last_ctr` 100 kalır, KM'de 0 satır, kayıt **kaybolmaz** (sayaç harcanmadı, kişi yeniden
      dokunabilir); `tags` kilidi ilerletmeden **sonra** (ayrı commit'ten sonra, yazımdan önce)
      alınırsa, ya da `locations` satırı herhangi bir anda tutulursa (ilerletme ona dokunmaz)
      `last_ctr` 101, KM'de 0 satır — kayıt kaybolur. Üretimde sınır 30 sn'lik istek zaman aşımıdır;
      aynı sınıf madde 18'in üçüncü kaynağıdır (orada `lock_timeout` ve yeniden deneme yok). Bugün tek site yazıcısı tap yoludur;
      API-1c'nin manuel saha kaydı ham yazıcı olacaktır. **Kural (deploy/README.md):** bu tablolara
      dokunan bir migration kendi `lock_timeout`'unu 2 sn'nin altına koyar.
17. **Üyelik bittikten sonra işverenin geçmiş saha satırları adsız (API-1c):** `holding_site_locations`
    katıdır (§6); üyelik bittiğinde işveren eşin mekânlarını okuyamaz ve geçmiş `site_*` satırları
    *"Another business's venue"* diye, vardiyasız (geç kalma hesaplanmaz) listelenir. Kayıt kaybolmaz
    (§4.6), yalnız adı ve vardiyası. Ad ve vardiyanın yazma anı anlık görüntüsü API-2'nin
    `punch_events`'idir (K-32).
18. **İzdüşümün kilit beklemesi (API-1c; denetimden sonra düzeltildi — E-ORTA-1).** `holding_feed`
    `site_punches`'a kendi `lock_timeout`'u (2 sn) altında yazar; beklemenin üç kaynağı var:
    - **`site_punches` üzerinde bir tablo kilidi** (bir okuyucunun arkasında kuyrukta bekleyen
      `ALTER TABLE`, `VACUUM FULL`, `CLUSTER`, elle `LOCK`) ve **sahanın `tenants` satırında bir
      `FOR UPDATE`** (izdüşümün anahtarı `KEY SHARE` alır). Denetim ürün yolunda ölçtü: 2,08 sn'de
      55P03, KM'de kayıt yok, KF sayacı ilerlemiş — §4.6. **Kapatılan:** checkin site satırını
      kapının 55P03'ündeki gibi `holding_feed`'in 55P03'ünde de savepoint içinde **bir kez** yeniden
      yazar (`TestHoldingWindow_ProjectionLockWaitIsWrittenOnceMore`; mutant: yeniden deneme yok →
      kayıt kayboldu). Atılabilir kapta `site_punches` 3 sn `SHARE` kilitliyken ilk deneme 2,02 sn'de
      55P03, ikinci deneme yazdı. **Kalan bedel:** kilit iki beklemeden uzun (> ~4 sn) tutulursa
      kayıt kaybolur, sayaç ilerlemiştir (`TestHoldingWindow_ProjectionLockHeldThroughBothWaitsIsTheNamedCost`)
      — kural `deploy/README.md`'de (bu tablolara dokunan migration kendi `lock_timeout`'unu 2 sn'nin
      altına koyar).
    - **Aynı anahtarlı commit edilmemiş bir red ikizi** (tekillik indeksi): ölçüldü 2,14 sn'de 55P03;
      **ürün yolunda erişilemez** — anahtar çalışanı içerir ve checkin aynı çalışanın her dokunuşunu
      kişi başına danışma kilidi altında sıraya sokar (`LockEmployeeForTap`); çalışansız holding
      satırı yoktur. Kalan yol ham bir yazıcıdır (§12 (d)).
    - **KF'nin `tags`/`locations` satırları — bu tetikleyicinin değil (ikinci denetim, ölçüldü):** bu
      satırlara `KEY SHARE`'i önce `transactions`'ın kendi anahtarları alır
      (`transactions_site_tag_fk`, `transactions_site_location_fk`); orada `lock_timeout` yok ve yeniden
      deneme yok — bir `FOR UPDATE` tutulduğu sürece dokunuş bekler, sınır isteğin zaman aşımıdır (30 sn;
      denetimde test bağlamının 120 sn'sine kadar asılı kaldı). M11'den eski sınıf, madde 16'nın gather
      cümlesiyle aynı; `holding_feed`'in anahtarları o satırlara geldiğinde kilit zaten bu işlemdedir.
    - **Onay kolu:** onay INSERT'i, KF plaket satırı başka bir işlemde `FOR UPDATE` tutulurken (ör.
      eşzamanlı bir ilerletme) izdüşümün anahtarında bekler; denetim 2,12 sn'de 55P03 ölçtü. Onay
      geri alınır ama **kaybolmaz** — kayıt değil yöneticinin kararıdır, panel hatayı söyler ve
      yönetici yeniden dener. Yeniden deneme yazılmadı (inceleme yolunun ilk kilit beklemesi; tap
      yolunun aksine sayaç gibi geri alınamaz bir şey harcanmadı).
19. **Manuel kayıt seçicisi eşin pasif mekânlarını da gösterir (API-1c):** `holding_site_locations`'ın
    dönüşünde `active` yoktur; eklemek kapalı listeyi genişletir (§2 — kullanıcı onayı). Pasif bir
    mekâna yazılan manuel kayıt yazılır ve sahada `employer_manual` olarak görünür.
20. **Geri doldurma yok (API-1c):** `00046`'dan önce yazılmış `site_*` satırları (yalnız M11
    geliştirme/CI veritabanlarında; üretimde holding satırı yok) izdüşüme taşınmadı — saha onları
    görmez. Üretimde `00046` holding üyeliğinden önce uygulanır (API-13 sırası).
21. **`holding_site_locations`'ın maliyeti (API-1c denetimi, E-D2, ölçüldü):** eş kümesini bütün
    `tenants` üzerinde `holding_peer` ile hesaplar — 21 bin tenant'ta çağrı başına 0,69–1,08 sn; manuel
    kayıt formu onu holding'siz işletmede de çağırır (üyelik uygulamada elde değil; yeni bir
    `holding_*` çağrısı kapalı listenin genişlemesi olurdu). Ön-süzme (`holding_members` üzerinden,
    karar yine `holding_peer`'de) bir migration ister — `docs/backlog.md` T124.
    *(API-8, 2026-10-15, `00055`)* **Holding'siz işletmenin yarısı kapandı:** `holding_site_locations` ve
    `holding_directory` eş kümesinden önce tek bir `is_member` sorar (§2 kip tablosu) — 21 bin tenant'ta
    0,68–0,79 sn → 1,0–1,4 ms. **Üyenin yarısı açık, adlandırılmış bedel:** bir üye çağrı başına 0,96–1,03 sn
    öder (KM'nin rehberinin her sayfası). `holding_members` üzerinden ön-süzme ölçüldü (5,2–5,8 ms) ama §2'nin
    *"`holding_members`'a değinen tek gövde `holding_peer`"* kuralına bir istisna ister — kullanıcı kararı (T124).
22. **`sun_valid`'in değeri türetilebilir (API-1c denetimi, güvenlik G-D1):** sütun `tappa_app`'ten
    saklıdır, ama bir `refused` satırın `ctr_gap > 0`'ı (yalnız ilerletilmiş dokunuşta dolu, K-1i) ve
    `ctr`'ının plaketin `last_ctr`'ına eşitliği *"imza geçerliydi"*yi söyler. K-1i'nin KF tarafındaki
    sinyalini korumak için kod değişmedi; K-1h'nin *"nedeni değil"*i sütun düzeyindedir.
23. **Akış sayacının kilit beklemesi (API-2; ADR 0028 §5 — madde 16 ve 18'in sınıfı).** Bir `ok` dokunuşun
    AFTER tetikleyicisi — site'sız satırda `punch_feed`, site satırında `holding_feed` (K-31'in iki sayaç
    satırı) — hedef tenant'ın `feed_counters` satırını commit'e kadar kilitler ve kendi `lock_timeout`'u
    (2 sn) altında bekler. Satırı daha uzun tutan bir işlem (takılı bir yazıcı, elle bir `FOR UPDATE`)
    INSERT'i 55P03 ile düşürüyordu — ilerletilmiş plaketin kaydı kaybolurdu. **Kapatılan:** checkin bu
    beklemeye düşebilen her satırı (site satırı ya da `punch_feed`'in olay yazdığı `ok`/nfc-qr/practice
    olmayan satır) savepoint içinde yazar ve `punch_feed`'in 55P03'ünde de **bir kez** yeniden dener
    (`holding_feed`'inki API-1c'den beri) — `TestFeedCounter_LockWaitIsWrittenOnceMore`,
    `TestFeedCounter_HoldingTapLockWaitIsWrittenOnceMore` (mutant: `punch_feed` koşulu yok → kayıt
    kayboldu, ölçüldü). **Kalan bedel:** satır iki beklemeden uzun (> ~4 sn) tutulursa kayıt kaybolur,
    sayaç ilerlemiştir (`TestFeedCounter_LockHeldThroughBothWaitsIsTheNamedCost`). **Sınırı:** tutma
    süresinin üst sınırı müşteri havuzunun takılı yazıcı zaman aşımlarıdır (`idle_in_transaction_session_timeout`
    30 sn, TCP keepalive 30/10/3 — açılışta okunur, `TestFeedTimeouts_ShownAtStartup`). **Holding'de
    tenant'lar arası:** KF'de sayacı tutan bir işlem KM'nin KF plaketindeki dokunuşlarını da bekletir
    (§8'in adlandırılmış bağımlılığı). Onay yolu (inceleme) aynı beklemede panel hatası verir, yönetici
    yeniden dener — kayıt değil karar, geri alınamaz bir şey harcanmadı (madde 18'in onay kolu gibi).
    Olay yazmayan satırlar (flag, red, ignored, manuel, practice) hiçbir sayaç kilidi almaz ve
    beklemez (`TestFeedCounter_RowsThatAreNoEventAreWrittenPlainly`). **Kural (deploy/README.md):**
    `feed_counters`'a ya da `punch_events`'e kilit alan bir migration kendi `lock_timeout`'unu 2 sn'nin
    altına koyar; `00050`'nin geri doldurma parçaları (keyset, ≤ 1 000 punch) bir tenant'ın sayacını en
    çok bir parça süresince tutar ve süre tenant'ın büyüklüğünden de istatistikten de bağımsızdır (ölçüldü,
    gerçek tablolarda tek tenant 650 000 satır, istatistiksiz, bayat istatistikli ve `ANALYZE`'lı üç durumda:
    tutma medyanı 64–76 ms, en uzun 0,10–0,32 sn; 2 820 canlı dokunuş sürerken 0 hata — ADR 0028
    "API-2 uygulama notu", üçüncü tur). **Tek sayaç kümesi:** bir işlemin ilk olayı kilitlediği sayaç
    kümesinin dışında olay yazan ikinci satır kilitten önce `SQLSTATE TF001` ile reddedilir — iki yönlü
    kilitlenme yerine yüksek sesli hata (aynı not, D-1). İşareti (`tappa.feed_counters`) önceden yazan bir
    işlem kontrolü atlatır ve **başka** tenant çiftlerinin holding dokunuşlarını 40P01'e sürükler; ürün kodunda
    onu yazan yol yoktur, redline R5c adı migration dışında reddeder (aynı not, üçüncü tur).

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
  yalnız **mekân adıyla** etiketlenir. *(API-1c, 2026-10-14: mevcut dönüşlerin hiçbiri şirket
  etiketi vermiyor — `holding_tap_site`'ın `api_code`'u bir plaket uid'i ister, raporun kovası
  plaketsizdir; kova **yalnız mekân adıyla** etiketlendi.)* `holding_site_locations`'ın (ya da herhangi bir holding
  fonksiyonunun) dönüşünü genişletmek §2'nin kuralına tabidir — **kullanıcı onayı + bu ADR'nin
  değişikliği**; tarihli bir not tek başına yetmez (1. tur düzeltmesi, güvenlik merceği).
- **KF'nin KM çalışanı için onay vermesi** (plan §8) — kapsam dışı.

## Test edilecekler (adlarıyla)

Henüz yazılmamış testlerin adları `Test` öneki olmadan verilir (`TestEveryNamedTestExists`
kuralı: belgede yalnız var olan testin adı yazılır); test yazıldığında önek eklenir ve bu satır
gerçek adla güncellenir. Önekli adlar var olan testlerdir.

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
  `TestHoldingOps_ConcurrentAddsForOneTenantOneWins` ·
  `TestHoldingOps_ConcurrentAddsOfTwoTenantsKeepOneTimezone` (boş holding'e farklı saat dilimli iki
  tenant eşzamanlı → tam biri eklenir; mutant: holding kilidi kaldırılmış → iki saat dilimli holding)
- `TestHoldingOps_OneMembershipAtATimeIncludingTheExitWindow` ·
  `TestHoldingOps_RejoinWaitsForTheToleranceWindow` (`left_at + 59 sn` ret, `+ 61 sn` kabul; iki
  60 sn kopyasının eşitliği) · `TestHoldingOps_LeftAtCannotBeUndone`
- `TestHoldingDefiner_RoleShape` (NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesiz; `proconfig` sabit)
- `TestHoldingDefiner_ColumnMatrix` (§3 matrisi, tablo × fiil × sütun) ·
  `TestHoldingDefiner_NeverSelectColumns` (*"asla SELECT"* listesi + `%_hash` katalog sorgusu)
- `TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate` (üyelik sütunlarında UPDATE `false`;
  `lock_anchor = true` → 23514; gövdelerde `UPDATE public.holding_members` yok)
- **`TestHoldingCatalog_TheFunctionsAndTheirExactSignatures`** (§2: bütün şemalarda nitelikli ad
  kümesi = imza tablosu; argüman ve dönüş metni birebir; nitelikli ad başına tek aşırı yükleme; her
  şemadaki her `holding\_%`'nin sahibi `tappa_holdingdefiner`; `prosecdef`/`provolatile`/`proconfig`;
  `aclexplode` ile EXECUTE **tam eşitliği**) — mutantlar: dönüşüne `static_ips` eklenmiş
  `holding_site_locations` → kırmızı; `tappa_operator`'a `EXECUTE` → kırmızı; başka şemada bir
  `holding_*` → kırmızı
- **`TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks`** (§2 kanarya: TEST-NET adresi ve GPS yalnız
  `holding_tap_site`'ın üç kanıt sütununda; mutant: `location_name || static_ips` → kırmızı)
- `TestHoldingPeer_InvokerVolatileOwnedByDefinerExecutableOnlyByOwner` ·
  `TestHoldingPeer_ModesAreAClosedSet` (bilinmeyen kip → hata; `a = b` → `false`) ·
  `TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers` (§2'nin gövde taraması: yorumları
  ayıklanmış `pg_get_functiondef`, bütün şemalar, `BEGIN ATOMIC` için `pg_depend` kenarları;
  mutantlar: yorumda, tırnaklı/büyük harfli tanımlayıcıda ve `BEGIN ATOMIC` gövdesinde
  `holding_members` → kırmızı) · `TestHoldingDefiner_NoDynamicSQLNoUnicodeIdentifiers` (definer
  gövdelerinde `EXECUTE` ve `U&"` yok) · `TestHoldingPeer_TolerantModesRequireOverlappingMemberships`
  (A çıktıktan sonra katılan B ile A, pencerede eş **değil**; birlikte üyeyken çıkan A ile B
  pencerede eş) · `TestHoldingFunctions_PeerSetComputedOnce` (çok satırlı fonksiyonlarda
  `holding_peer` veri satırı başına çağrılmaz — plan/çağrı sayımıyla) ·
  `TestHoldingPeer_OnlyRowLockIsForShare` (`holding_peer`'de `FOR UPDATE` / `FOR NO KEY UPDATE` /
  `FOR KEY SHARE` / `UPDATE` / `INSERT` / `DELETE` / `MERGE` yok; mutant: ters sırada `FOR UPDATE`
  → kilitlenme)
- `TestHoldingFunctions_ExecuteGrantedOnlyToApp` · `TestHoldingTriggers_ExecuteOnlyOwnerAndAssertRelid`
  (geçici tablo bağlama reddi)
- `TestRestore_FreshPodCarriesTheHoldingRole` (sıfırdan pod; `01-roles.sql` idempotent)

**Çapraz dokunuş (API-1b)** — harf, plan §5 API-1b kabulünün maddesidir *(API-1b'de yazıldı,
2026-10-14 — gerçek adlar; devredilen yarılar adıyla ve nedeniyle, *"API-1b uygulama notu"*)*
- (a) `TestHoldingTap_RecordLandsAtTheEmployerWithSiteColumns`
- (b) `TestHoldingTap_OutsiderSessionOnAPeerPlaqueIsRefusedAndCounterUnchanged` ·
  `TestHoldingTap_PeerSessionOnAnOutsiderPlaqueLeavesItsCounter` ·
  `TestHoldingFunctions_ReturnNothingForAnOutsiderOrAnOutsiderKey` ·
  `TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers` (C, H2'nin üyesi; mutantlar fonksiyonlarda
  **ve** `holding_site_gate`'te kırmızı — API-1c'den beri `holding_site_locations`'ta ve ikinci
  tetikleyici `holding_feed`'in onay kolunda da) · `TestHoldingManual_PeerManagerCannotNameAnOutsiderLocation`
  (C'yi saha diye adlandıran satır hiç yazılamaz; *"C'nin `site_punches`'ı 0"* yarısı
  `TestSitePunches_OutsiderSeesNothing`) ·
  `TestHoldingDirectory_ReturnsPeersNeverTheCaller` (*API-1b'den devredildi, API-8'de yazıldı* — `holding_directory` orada doğdu;
  API-8'in öteki adları aşağıda, *"API-8 uygulama notu"*)
- (c) `TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins` (SQL düzeyinde, iki ifade yarı/yarı) ·
  `TestHoldingTap_ConcurrentSameCounterExactlyOneSUNValid` (ürün yolundan, `Service.Record`) — `-race`
- (d) `TestDecide_PersonDebounceSpansBothTenantsPlaques` · `TestHoldingTap_PersonDebounceSpansBothTenantsPlaques` ·
  `TestHoldingTap_DirectionChainStaysAtTheEmployer`
- (e) `TestHoldingTap_SiteLocationExceptionsDoNotApply`
- (f) `TestDecide_CrossTenantQRWithoutSiteIPIsFlagged` (işveren QR kuralını gevşetse bile) ·
  `TestDecide_CrossTenantQRWithSiteIPIsOK` · `TestGuardrails_CrossTenantQRSitsRightAfterDebounce` ·
  `TestGuardrails_CrossTenantQRCannotBeLoosenedOrTightened` · `TestHoldingTap_CrossTenantQRNeedsTheSiteIP`
- (g) `TestHoldingWindow_AdvancedBeforeLeftWrittenAfterIsRecorded` ·
  `TestHoldingWindow_LeftBetweenAdvanceAndGatherFlagsWithoutSiteEvidence` ·
  `TestHoldingWindow_AdvanceAfterLeftIsNotPeer` · `TestHoldingAdvance_EqualCounterIsReplayWithSiteColumns` ·
  `TestHoldingTap_LostPlaqueRefusalRecordedAtEmployerSeenAtSite` (KF'nin `refused_tag_not_active`
  görünümü API-1c'nin) · `TestHoldingAdvance_InactivePlaqueIsPeerInactiveNot403` ·
  `TestHoldingWindow_RefusalsSurviveTheLeftRace` (replay, `peer_inactive`, kayıp plaket) ·
  `TestHoldingAdvance_DecideSeesTheCurrentStatus` (kanca ile kurulan yarış) ·
  `TestHoldingAdvance_StatusAtTheAdvanceOutranksTheLaterRead` (ilerletmenin durumu katı okumanınkinden önce) ·
  `TestHoldingWindow_GateLockWaitIsWrittenOnceMore` · `TestHoldingWindow_GateLockHeldThroughBothWaitsIsTheNamedCost`
  (kapının `lock_timeout`'u — §6, §13 madde 16) ·
  `TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter` (FK beklemesi + commit edilmemiş `op_remove`;
  `FOR SHARE` sıralaması; *"AFTER KF tarafını atlamaz"* yarısı API-1c'nin) · `TestHoldingWindow_QRRefusalSurvivesTheLeftRace` ·
  `TestHoldingWindow_UnadvancedNFCRowAfterLeftIsRefused` · `TestHoldingWindow_QRDecisionAfterLeftIs403` ·
  `TestHoldingWindow_ManualAfterLeftIsRefused` · `TestHoldingReads_AfterLeftReturnNothing` ·
  `TestHoldingWindow_ToleranceIsAtLeastTwiceTheRequestTimeout` · `TestHoldingWindow_GateArms` (kapının
  iki kolu satır satır, 10 sn ve 61 sn) · `TestHoldingPeer_AppCannotCallIt` (katalog satırı)
- (h) `TestHoldingActivation_OnlyOnItsOwnTenantsPlaque` ·
  `TestHoldingTap_PendingActivationBrowserWithLiveSessionTapsAsHolding` ·
  `TestHoldingTap_PendingActivationOnTheInvitationsPlaqueStillActivates`
- (i) `TestHoldingBrand_TapAndResultShowTheSiteBrand` · `TestHoldingLogo_VIESGate` ·
  `TestHoldingLogo_OutsiderOrStaleDigestIs404` · `TestHoldingTapSite_LogoOnlyForAVIESVerifiedSite`
- (j) `TestHoldingCheck_ChannelRules` (lokasyonsuz plaket reddi yazılır; manuel saha yazılır;
  `site_tenant_id = tenant_id` ret; `site_tenant_id` NULL iken `site_*` dolu ret)
- (k) `TestHoldingMigration_AlterWithinTwoSeconds` (ölçüm; `NOT VALID` yolu seçildi)
- (l) `TestHoldingAdvance_KeepsReplayProtection` (eşit/geri `ctr` → `replay`, sayaç değişmez;
  holding dışı → `not_peer`; `ctr_gap` yalnız `advanced`'ta) · `TestHoldingAdvance_CounterGapReachesTheEmployersReview`
  (K-1i: KF@100, KM 150 → `base:ctr-gap-review`; ardından KF 151 → boşluk yok)
- `TestDecide_TenantMismatchRespectsSameHolding` (CLAUDE.md §5'in `TestDecide_…` kuralı)
- Ek (API-1b): `TestAdvance_APeersPlaqueGoesThroughTheHoldingStatementOnly` ·
  `TestHoldingDefiner_EveryGrantIsUsed` · `TestHolding00043And00044_DownGiveBackAndUpTakesItAgain`
  (`00045`'in Down/Up'ı dahil) · `TestHoldingSite_EvidenceLeavesByNoRoute` (K-39, Go tarafı) ·
  `TestCheckin_SiteMembershipEndedIs409TapAgain`

**İzdüşüm ve paneller (API-1c)** *(API-1c'de yazıldı, 2026-10-14 — gerçek adlar; ölçümler ve
mutantlar *"API-1c uygulama notu"*nda)*
- `TestSitePunches_CatalogCarriesNoEvidenceColumns` · `TestSitePunches_OutcomeMappingIsTotal`
  (`ignored` yazılmaz, işverenin kaydı geri alınmaz)
- `TestSitePunches_ReplayURLCollapsesAtTheSite` (N → KF'de 1, işverende N) ·
  `TestSitePunches_DifferentEmployeesReplaysStaySeparate` ·
  `TestSitePunches_ForgedCountersCollapsePerHourBucket` (200 sahte URL) ·
  `TestSitePunches_RealRefusalsAreKeptOneEach` (3 → 3) ·
  `TestSitePunches_LostPlaqueRepeatsGiveOneRowAndOneAlert` ·
  `TestSitePunches_LostPlaqueAlertCannotBeSuppressed` ·
  `TestSitePunches_BucketIsServerClockAndZoneFree` (UTC, Europe/Malta ve Asia/Kolkata aynı kova) ·
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
- Ek (API-1c): `TestSitePunches_AppendOnly` · `TestSitePunches_CounterGapComesFromTheEmployersRow` ·
  `TestHoldingFeed_ReadsOnlyTheCounterGapOfThePolicyContext` (K-1i'nin gövde taraması) ·
  `TestHoldingFeed_NameOrderIsFirst` · `TestHolding00046And00047_DownGiveBackAndUpTakesItAgain` ·
  `TestHoldingManual_RecordAtAPeersVenue` ·
  `TestHoldingManual_PeerManagerCannotNameAnOutsiderLocationThroughThePanel` ·
  `TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason`; API-1b'den devralınan yarılar var olan
  testlere eklendi: `TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers` (`holding_feed`,
  `holding_site_locations`), `TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter` (AFTER yarısı),
  `TestHoldingTap_LostPlaqueRefusalRecordedAtEmployerSeenAtSite` (KF'nin `refused_tag_not_active`'i),
  `TestHoldingAdvance_CounterGapReachesTheEmployersReview` (K-1i'nin saha yarısı)

**Holding tetikleyicisi (API-2'nin holding satırları)** *(API-2'de yazıldı, 2026-10-14 — ADR 0028
"API-2 uygulama notu"; önerilen adlar ADR 0028'in (j), (p), (q), (m) testleriyle birleşti)*
- KM@KF'nin iki hedef satırı ve anlık görüntüsü (KM satırında KF lokasyon id/ref/ad/şirket kodu, KF
  satırında KM çalışan id/ref/kod/şirket kodu), kendi-tenant tetikleyicisinin site satırına ve onayına
  hiç dokunmaması, işveren olayının tam bir kez: `TestFeed_TargetRowsAndSnapshots`
- `TestFeed_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce` (karşı taraf NULL; KF atlanır, hata yok)
- `TestFeed_MissingCounterRowsNoDeadlock` (*"var et"*siz mutant: `TestFeedMutant_PreLockWithoutMakeExistDeadlocks`) ·
  ad sırası `TestHoldingFeed_NameOrderIsFirst` + `TestFeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid`
  (holding ilk, `punch_feed` ikinci) · yazılan dört tablonun sütun pini
  `TestFeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid` (+ `site_punches` için
  `TestSitePunches_CatalogCarriesNoEvidenceColumns`) · kanarya dört tabloda
  `TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks` · K-33 mutantları olay dalında
  `TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers` · matrisin API-2 satırları
  `TestHoldingDefiner_ColumnMatrix` + `TestHoldingDefiner_EveryGrantIsUsed`

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

## API-1a uygulama notu (2026-10-13)

Yapıcının uygulaması ve ölçümleri; ADR'nin kararları değişmedi, sapmalar ve ADR'nin açık bıraktığı
kararlar aşağıda adıyla. Atılabilir PostgreSQL 17.10 kaplarında ölçüldü (paylaşılan veritabanlarına
yıkıcı hiçbir şey yapılmadı; kaplar silindi).

**Üç migration, iki parça.** `00040` göç defterini (`goose_db_version`) `tappa_app`'e kapatır (API-1
denetiminin devri — ADR 0028 *"API-1 uygulama notu"*). Holding **ikiye bölündü** — `00041` şema
(`holdings`, `holding_members`, `tappa_holdingdefiner`'ın §3 satırı, `holding_peer`, saat dilimi
tetikleyicisi), `00042` operatör yüzeyi (üç yazar, okuma, türler, `op_enable_tenant_api`). **Neden
(ölçüldü):** `00041`'in Down'ı `tenants` üzerinde ACCESS EXCLUSIVE alır (`DROP TRIGGER` ve
`holding_members`'ın `tenants`'a FK'sini düşüren `DROP TABLE` ikisi de); `internal/db`'nin operatör
testleri eski bir sürüme `opAtVersion` ile sonraki Down'ları kendi işleminde koşarak iner ve tek
dosyada bu kilit, koşu boyunca `tenants` okuyan her paketi bekletiyordu (ilk tam koşuda
`TestOperator00034_DownGivesBack00033AndUpTakesItAgain` T9 ölçümüyle düştü). `00041` hiçbir operatör nesnesi adlandırmaz,
`opAtVersion` onu (00037/00038 emsaliyle) atlar; `00042`'nin Down'ı `tenants`'ı kilitlemez (ölçüldü).
`00040` da atlanır.

**Tablolar.** `holdings(id DEFAULT gen_random_uuid(), name, created_at DEFAULT clock_timestamp())`,
ad kuralı `holdings_name_shape` (kırpılmış, 1–120 karakter, kontrol karakteri yok — `op_create_holding`
aynı kuralı 22023 ile önce sınar). `holding_members` birincil anahtarı `(member_tenant_id,
joined_at)` (yeniden katılım yeni satırdır), `joined_at DEFAULT clock_timestamp()` ve INSERT listesinde
yok, `left_at >= joined_at`, `lock_anchor` CHECK'i, ve **kısmi UNIQUE `(member_tenant_id) WHERE left_at
IS NULL`** — *"aynı anda tek üyelik"*in yapısal arka kemeri (60 sn'lik pencere `op_add_holding_member`'ın
kuralıdır; indeks saati göremez). İki tablo ENABLE + FORCE RLS ve **hiç politikası yok**: RLS'e uyan
hiçbir rol okuyamaz (geri yükleme artığı dahil); iki BYPASSRLS definer sütun grant'larıyla erişir.
`tappa_app`'ten açık `REVOKE ALL`; R5 muafiyetleri `00041`'in Up'ında. M11 katalog testinde
*"tenant kapsamsız"* tablolar için kural *"ENABLE + FORCE ve politika YOK"* olarak eklendi.

**`tappa_holdingdefiner`'ın yetkisi: yalnız §3'ün `holding_members` satırı** (SELECT
`holding_id, member_tenant_id, joined_at, left_at`; UPDATE `lock_anchor`). Matrisin öteki satırları
fonksiyonlarıyla birlikte (API-1b/1c/2) ölçülerek kazanılır — §3'ün *"üst sınır"* kuralı.
`TestHoldingDefiner_ColumnMatrix` rolün bütün erişimini (`public`'teki her ilişki × fiil × sütun,
diziler, şemalar, başka rollerin fonksiyonlarında EXECUTE) bu satıra eşitler.

**`holding_peer`.** Beş kip §2'deki gibi; açık bırakılanlar: `lock` kipi **NULL** döner (*"karar
vermez"*); `is_member` ikinci bir tenant alırsa 22023; NULL tenant ve `a = b` her karar kipinde
`false`. Gövde PL/pgSQL; kilit, aday satırları `ORDER BY member_tenant_id, joined_at … FOR SHARE OF m`
ile alır (kilitleme `Sort`'un üstünde koşar — kanonik sıra). **Yeniden ölçülen iddialar**
(`TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate`, `TestHoldingPeer_OnlyRowLockIsForShare`):
`UPDATE (lock_anchor)` geri alınınca `lock` kipi 42501 (satır kilidi bir sütunda UPDATE ister);
fonksiyon STABLE yapılınca 0A000; ters argüman sırasıyla iki `lock` çağrısı birbirini beklemez; bir
çıkarıcının `left_at` yazması bu paylaşılan kilidi bekler; **mutant** (aynı satırlar `FOR UPDATE` ile,
ters sırada, birer birer) 40P01 kilitlenme. Örtüşme şartı kaldırılmış mutant
`TestHoldingPeer_TolerantModesRequireOverlappingMemberships`'te kırmızı.

**Saat dilimi kilidi.** `holding_timezone_lock()` — SECURITY DEFINER, sahibi `tappa_holdingdefiner`,
`proconfig` = `search_path` + `lock_timeout=2s` (ADR 0028 §5'in başlangıç değeri), `EXECUTE` yalnız
sahipte, `TG_RELID`/`TG_WHEN`/`TG_LEVEL`/`TG_OP` assert'i (yanlış bağlama 55000); tetikleyici
`tenants_holding_timezone_lock BEFORE UPDATE … WHEN (OLD.timezone IS DISTINCT FROM NEW.timezone)`. Ret
**23514**, `CONSTRAINT = 'holding_timezone_lock'` ile; `internal/domain/tenant` bunu
`ErrTimezoneHeldByHolding` olarak tanır ve owner paneli zaman dilimi alanının yanında **bir cümle**
söyler (*"This business shares one clock with the other businesses in its group on Taptime, or left that
group less than 60 seconds ago, so its timezone cannot be changed here."* — API-1a denetimi iki cümleyi
bire indirdi ve yeniden denetimi çıkış penceresini ekledi: kilit `is_member` penceresinden gelir, çıkıştan
sonra 60 sn daha sürer; v1'de bütün üyelerin saat dilimini taşıyan bir yol olmadığı için cümle bir yol
göstermez). Uygulama üyeliği
önceden soramaz (`tappa_app` üyelik okumaz) — cümle ret anında gelir. Tetikleyici devre dışıyken
`TestHoldingTimezone_OwnerCannotChangeAMembersTimezone` kırmızı.

**Üç yazar — ret SESSİZDİR (ADR 0021 §2 v 7, B14; §1'in *"durum kehaneti yok"*'u).**
`op_add_holding_member` bilinmeyen holding/tenant, canlı ya da penceredeki üyelik (herhangi bir
holding'de), farklı saat dilimi (canlı **ve penceredeki** üyelere göre — penceredeki satırlar hâlâ eski
saat dilimiyle hesaplanır) ve bir üyenin taşıdığı API kodu durumlarında **eklemez** ve eklediği zamanki
`void`'u döner; `op_remove_holding_member` canlı üyelik yoksa aynı `void`'u döner. §1'in *"reddeder"*i
budur: üyelik yazılmaz. Operatör satırı her kabul edilen çağrıda yazılır (00039'un kuralı: satır
istenen eylemi kaydeder), tenant satırları yalnız üyelik değiştiğinde. Operatör sonucu iki aşamalı
okumayla görür (aşağıda). ⚠️ **Sayılı, kapatılmadı** (ADR 0021 sınır 15, LV3 sınıfı): `SAVEPOINT` içinde
koşulup geri alınan bir çağrı, istatistik görünümleri ya da bir kilit beklemesi üzerinden bir
tenant'ın katılıp katılamayacağını (saat dilimi, üyelik, kod) söyler; commit edilen her çağrı operatör
satırını bırakır. **Gözlem (ölçüldü):** aynı operatör oturumunun eşzamanlı çağrıları
`op_touch_session`'ın oturum satırını yazmasıyla birbirini bekler; eşzamanlılık testleri bu yüzden
çağıran başına **ayrı oturum** kullanır (tek oturumla kilit mutantları yeşil kalıyordu).

**Audit.** Operatör türleri `holding_created` (tenant yok), `holding_member_added`,
`holding_member_removed` (tenant'ı adlandırır); üçü de `detail = {"holding_id": "<uuid>"}` ve başka hiçbir
şey taşımaz — `operator_audit_log_holding_write_shape` CHECK'i; `op_read_audit` bu biçimi tanır (değeri
ekrana okumaz). Tenant satırları (K6): eklenen `tenant.holding_joined`, diğer canlı üyeler
`tenant.holding_member_joined` (`member_tenant_id` = katılan); çıkan `tenant.holding_left`, kalan canlı
üyeler `tenant.holding_member_left`; hepsi `{actor_kind: "operator", holding_id[, member_tenant_id]}`,
`target` = holding id'si, `at` = üyeliğin `joined_at`/`left_at` anı (tek saat okuması).

**Okuma — `op_read_holdings` (yapıcının otonom kararı; ADR 0021 sınıfı, yeni bir `op_*`).** Ekranın
üyeleri gösterebilmesi ve v 7'nin *"sonucu görmek isteyen iki aşamalı bir okuma yapar"*ı için:
`op_begin_read('holdings', {page_number, page_size})` + `op_read_holdings` — üyelik başına bir satır
(canlılar önce, sonra bitmişler) ve hiç üyesi olmamış holding için üye sütunları NULL bir satır; üye
tenant'tan yalnız id, ad, saat dilimi, API kodu (eklenmeyen bir tenant'ın nedenini açıklayan üç alan);
en çok 200 satır. Okuma türü `operator_read_tickets_kind_check`'e eklendi. Bu, §2'nin **kapalı
`holding_*` listesini genişletmez** (o liste `tappa_holdingdefiner`'ındır); `op_*` sınıfına bir okuma
ekler — ADR 0021'in M11 notuna yazıldı.

**`left_at` geri alınamaz — tetikleyiciyle değil, yetki ve gövde pini ile.** Bir koruma tetikleyicisi
gövdesinde `holding_members`'ı adlandırırdı (`TG_RELID` assert'i) ve §2'nin *"holding_members'a değinen
tek fonksiyon"* taraması onu haklı olarak reddederdi. Bunun yerine: üyelik sütunlarında UPDATE yalnız
`tappa_opdefiner`'da ve yalnız `left_at`'te; `tappa_opdefiner`'ın bütün gövdelerinde `holding_members`'a
tek UPDATE var, `SET left_at = clock_timestamp()` ve `WHERE … left_at IS NULL` ile
(`TestHoldingOps_LeftAtCannotBeUndone` pinler). Göç rolü (süper kullanıcı) bu kuralın dışındadır —
her yerde olduğu gibi.

**60 sn'nin üç kopyası:** `holding_peer` (tolerant ve `is_member`), `op_add_holding_member` ve
`op_enable_tenant_api` (kod kuralının penceresi) — `TestHoldingOps_RejoinWaitsForTheToleranceWindow`
gövdelerdeki her `interval` sabitini `'60 seconds'`a eşitler ve +59/+61 sn'yi davranışla ölçer.

**Mutant kanıtları (atılabilir kapta fonksiyon gövdesi değiştirilip test koşuldu, sonra geri
yüklendi):** danışma kilidi yok → iki saat dilimli holding (`TestHoldingOps_ConcurrentAddsOfTwoTenantsKeepOneTimezone`
1. turda kırmızı); `tenants` satır kilitleri yok → iki sırada da farklı saat dilimli üye ve tek tenant
için eşzamanlı eklemede unique ihlali (`…TimezoneComparisonRacesTheOwnersUpdate`,
`…ConcurrentAddsForOneTenantOneWins` kırmızı); pencere şartı yok → `…OneMembershipAtATime…` ve
`…RejoinWaits…` kırmızı; kod şartı yok (iki yön) → iki kod testi kırmızı; `op_enable_tenant_api`'de
**danışma kilidi ve eş satır kilitleri birlikte** kaldırılınca iki eş aynı kodu alır (biri tek başına
kaldırılınca öteki sıralamayı tutuyor — ölçüldü; ikisi de korunur: danışma kilidi ADR'nin tek
serileştirme noktası, satır kilitleri kanonik sıra); hiç kilit yoksa *"ekle, sonra kod"* sırası da
kırmızı.

**Operatör ekranı (`/operator/holdings`).** Liste `op_read_holdings` ile okunur (okuma bütçesinden bir
birim; `?page=N` yalnız 1–1000 düz rakam, başkası store'dan önce 404); holding oluşturma ve üye
ekleme/çıkarma `db.OperatorDB` üzerinden yalnız üç `op_*`'i çağırır ve okuma harcamaz. Ret sessiz
olduğu için yazma 303 ile listeye döner ve altındaki cümle **POST'tan değil o okumadan** türetilir:
*"Added."* yalnız okuma canlı üyeliği gösterince, sayfa kenarı sonucu belirsiz bırakınca *"The result is
not on this page."* (`TestHoldings_TheResultIsReadNotPosted`). Ad ve id'ler sınırda doğrulanır, sayfaya
geri yazılmaz (reddedilen form 422 ile listeyi yeniden okur). ⚠️ **Sayılı sınırlar:** (1) *"Added."*/
*"Removed."* bu çağrıyı daha önceki bir çağrıdan ayıramaz — okuma çağrıyı değil üyeliğin son durumunu
döndürür; (2) audit ekranı holding satırlarında **hangi** holding olduğunu göstermez — `op_read_audit`
`holding_id`'yi tanır ama `db.OperatorAuditEntry`'de alanı yok (ayrı iş). Uçtan uca:
`TestE2E_HoldingsCreateAddReadRemoveThroughTheDefiners`.

**Geri yükleme.** Rol `01-roles.sql`'in yeni `HOLDING ROLE (M11 API-1a)` bloğundadır ve canlı küme
için `deploy/README.md` → *"Holding role (M11 API-1a)"*; `scripts/db-init`'ten sıfırdan kurulan bir kapta
rol vardı ve goose 42'ye çıktı (ölçüldü); `TestRestore_FreshPodCarriesTheHoldingRole` bloğu rolsüz
kümede koşar.

**Denetim turu (2026-10-14) — yalnız test, metin ve yorum; hiçbir migration ya da fonksiyon
davranışı değişmedi.** Üçüncü gözün iki ORTA'sı ve iki denetçinin DÜŞÜK'leri:
- **`holding_peer`'in kip kümesi pinli** (ORTA-1: altıncı bir `'any'` kipi her testte yeşil kalıyordu).
  `TestHoldingPeer_ModesAreAClosedSet` gövdeyi `md5(prosrc)` ile sabitler, korumanın listesini tam beşe
  eşitler, `mod`'un karşılaştırıldığı her kelimeyi beşle sınırlar ve beşinin her birinin kabul
  edildiğini iki canlı eşle gösterir; hata mesajı değişikliğin bu ADR'nin değişmesini ve kullanıcı
  onayını (CLAUDE.md §4.5) istediğini söyler. Mutant (`'any'`, M3 biçiminde) → kırmızı.
- **`op_remove_holding_member`'ın danışma kilidi bir yarış testiyle tutuluyor** (ORTA-2):
  `TestHoldingOps_RemoveWaitsForAnUncommittedAdd` — Y'nin eklemesi commit edilmemişken X çıkarılır;
  çıkarıcı `pg_locks`'ta danışma kilidini bekler ve Y'nin izine X'i adlandıran
  `tenant.holding_member_left` düşer. Mutant (kilit yok) → beklemez ve Y'nin satırı yok: kırmızı.
- **`PeerSetComputedOnce` plana bağlı değil** (DÜŞÜK-1): eşe ilk veri satırları ilk sayımdan önce
  verilir; boş bir `locations`'ta hash join'in boş tarafı CTE'yi hiç okumuyor ya da ilk eşte
  bırakıyordu (seed'siz kapta 8/8 kırmızı, düzeltmeden sonra 8/8 yeşil).
- **Tetikleyicinin bağlanışı pinli** (DÜŞÜK-2): `pg_get_triggerdef`, `WHEN` dahil, etkin ve tek
  (`TestHoldingTriggers_ExecuteOnlyOwnerAndAssertRelid`); `WHEN`'siz bağlama → kırmızı.
- **Metinler bugün doğru olanı söylüyor** (DÜŞÜK-3): *"Removed."* altında *"The business can join a
  holding again, and its owner can change its timezone, after 60 seconds."*; ekranın girişi API-1b'nin
  tap yolunu anlatmıyor ve saat dilimi kilidinin çıkıştan sonra 60 sn sürdüğünü söylüyor; owner
  panelinin K-36 metni tek cümle ve pencereyi de söylüyor (yeniden denetimin DÜŞÜK'ü).
- **`left_at` taraması tırnaklı adı da okuyor** (DÜŞÜK-5): `UPDATE public."holding_members"` artık
  kaçamıyor; tabloyu adlandıran her UPDATE ayrıntılı desenle okunmak zorunda. Mutant → kırmızı.
- Yorumlardaki yanlış migration numarası (00041 → 00042) düzeltildi (DÜŞÜK-4).

## API-1b uygulama notu (2026-10-14)

Yapıcının uygulaması ve ölçümleri; ADR'nin kararları değişmedi — sapmalar ve ADR'nin açık bıraktığı
kararlar aşağıda adıyla. Yıkıcı ölçümler ve mutantlar atılabilir PostgreSQL 17.10 kaplarında koşuldu
(paylaşılan veritabanlarına DDL yapılmadı; kaplar silindi).

**İki migration.** `00043` sütunları, dört CHECK'i ve iki FK'yi **`NOT VALID`** ekler, kısmi indeksi,
`tappa_holdingdefiner`'ın dört tablodaki yetkilerini, üç çağrılabilir fonksiyonu ve kapıyı kurar;
`00044` altı kısıtı **kendi işleminde** `VALIDATE` eder. Down'lar: `00044` altı kısıtı aynı metinle
`NOT VALID` geri ekler (VALIDATE'in tersi olan durum); `00043` bir satır site adlandırdığı sürece
**reddeder** ve hiçbir şey değiştirmez (kaydın *"nerede"*si silinmez — CLAUDE.md §4.3/§4.6; `00037`
emsali). İkisi de operatör nesnesi adlandırmaz, `opAtVersion` ikisini atlar.

**(k) ALTER süresi — ölçüm ve yol.** Üretim biçimli satırlarla (ortalama 582 bayt) doldurulmuş atılabilir
kapta, kısıtları ALTER içinde doğrulayan ilk biçim `transactions`'ı ACCESS EXCLUSIVE altında tuttu:
688 000 satırda **0,68–0,90 sn**, 2 000 000 satırda **2,06–3,46 sn** (üç koşu). Parçalar (2 M, sıcak):
dört CHECK 0,64 sn, iki FK 0,58 + 0,56 sn, indeks 0,35 sn. Eşik tablonun öngörülebilir boyutu içinde
aşıldığı için **`NOT VALID` yolu**: `00043` 2 M satırda 0,39–0,46 sn (tek tarama indeks yapımı, boş bir
kısmi indeks), `00044` 2,8–3,3 sn ama SHARE UPDATE EXCLUSIVE altında — doğrulama sürerken bir
`tappa_app` INSERT'i beklemeden yazıldı (ölçüldü). Mevcut her satırda üç site sütunu NULL'dur ve altı
kısıt bunu kabul eder; `NOT VALID` kısıt bundan sonraki her satırda zorlanır — yani ara durumda hiçbir
şey açık kalmaz. `TestHoldingMigration_AlterWithinTwoSeconds` bunu **tarama sayısıyla** pinler
(`pg_stat_xact_user_tables.seq_scan`): `00043`'ün ALTER'ı karalama kopyada 0 tarama, indeks 1, `00044`'ün
VALIDATE'leri > 0; kontrol olarak `NOT VALID`'siz biçim tarar.

**Kapı (`holding_site_gate`).** §6'nın modeli: önce `holding_peer(işveren, saha, 'lock')`, sonra kol.
Kol (i) **`channel = 'nfc'`** ile daraltıldı (§6'nın metni kanal adlandırmıyordu; QR'ın `sun_valid`'i hep
yanlıştır, ham bir yazıcının `sun_valid=true` bir QR satırı toleransa girmesin). Bu daraltma
`TestHoldingWindow_GateArms`'ın `QRAdvancedShapeAfterLeftIsRefused` satırıyla pinli (QR, `sun_valid`
doğru, `ctr ≤ last_ctr`, 10 sn → ret): kanal şartı çıkarılınca kırmızı — ilk turda bu mutant yeşildi
(denetim, üçüncü göz madde 1). Ret 23514,
`CONSTRAINT = 'holding_site_gate'`; checkin bunu `ErrSiteMembershipEnded` diye tanır, ekran 409 +
*"tekrar dokunun"* (`tapProblemServer`; hiçbir şey yazılmadı). **FK beklemesi ölçüldü:** kapının
`FOR SHARE`'i satırın commit'ine kadar tutulur, bu yüzden FK denetimi plakette beklerken bir
çıkarıcının `left_at` yazması **o da bekler** — üyelik, kapının kararı ile satırın commit'i arasında
bitemez (`TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter`, *"the key wait"*). Mutantlar: `lock`
çağrısı yok → commit edilmemiş `op_remove` ile kesişen katı satır beklemeden **yazıldı**; her satır
toleranslı / hiçbiri toleranslı / kol (ii)'nin sayaç sınırı yok → ilgili testler kırmızı.

**`holding_advance_tag_counter`.** Tek ifade, `db/queries/tags.sql`'in kalıbı: `peer` (bir kez,
materyalize) `prev`'de ve `UPDATE`'te, `prev` `FOR UPDATE OF g`, `v.old_ctr < p_ctr`, `old_status =
'active'`. **Açık kalan bir sözleşme noktası, kararıyla:** §2'nin 3. maddesi *"bağlam yoksa 0 satır"*
der ama imza `not_peer`'i bir sonuç olarak adlandırır; fonksiyon **her zaman tek satır** döner ve eş
olmayan, bilinmeyen uid ve bağlamsız çağrının cevabı aynıdır: `('not_peer', NULL, NULL)` (kehanet
yok) — denetim onay gerektirmediğine hükmetti, §2'ye dipnot (¹) olarak yazıldı. Durum
sütunu kilitteki durumdur (`prev`'in `FOR UPDATE`'i READ COMMITTED'de satırı yeniden okur). Mutantlar:
`<` → `<=` (eşit sayaç ilerledi; dört test kırmızı), `FOR UPDATE` yok (iki ifadeli yarışta birden
fazla kazanan — SQL düzeyindeki yarış testi kırmızı; ürün yolundan 20 Record'luk yarış bu mutantı
**görmedi**: istekler arasındaki gidiş dönüşler pencereyi kapatıyor — deponun bilinen dersi, pin SQL
düzeyindedir), `peer` süzgeci kaldırıldı (C'nin sayacı KM'den ilerledi; beş test kırmızı). **Yarış
testinin tek-tenant yarısı ürünün kendi sorgusudur** (denetim, üçüncü göz madde 7): ilk biçim
`store.AdvanceTagCounter`'ın SQL'inin bir **kopyasını** koşuyordu — kopya, gerçek sorgu kilidini
kaybetse de yeşil kalırdı. Artık `store.New(tx).AdvanceTagCounter`; `internal/store`'daki sorgudan
`FOR UPDATE` çıkarılınca test 3/3 koşuda kırmızı (20 yarışçıda 6–10 kazanan).

**`holding_tap_site` / `holding_logo`.** `business_name` de logo ile birlikte kapılıdır (yalnız
VIES-doğrulanmış sahada; §2 *"logo alt'ı olan işletme adı"*) — daha dar. Accent her durumda döner.
Mutantlar: logo özeti VIES'siz → `TestHoldingTapSite_LogoOnlyForAVIESVerifiedSite` ve tap ekranı testi
kırmızı (bu mutant ilk turda **yeşil kalmıştı**; test bu yüzden eklendi); `holding_logo`'da VIES ya da
özet şartı yok → logo rotası testleri kırmızı; okuma `tolerant_stmt` → `TestHoldingReads_AfterLeftReturnNothing`
kırmızı. Tek satırlı üç fonksiyonun `holding_peer`'i çağrı başına **tam bir kez** sorduğu sayılır
(`TestHoldingFunctions_PeerSetComputedOnce`, `holdingSingleRowCalls`).

**Yetki matrisi — her sütun ölçülerek.** `tags` SELECT (`uid, tenant_id, location_id, last_ctr, status`)
+ UPDATE (`last_ctr`); `locations` SELECT (on sütun); `tenants` SELECT (`id, name, timezone,
vat_verified, api_code`); `tenant_branding` SELECT (yedi sütun). `TestHoldingDefiner_EveryGrantIsUsed`
her sütunu **tek başına** geri alıp üç fonksiyonu ve kapıyı sürer: her geri alma en az bir 42501 üretir
(gereksiz sütun yok). `tenants.api_enabled_at`, `employees`, `transactions` satırları API-1c/API-2'nin.

**Tap yolu (checkin).** Başka tenant'ın plaketi `advanceAtAPeer`'e gider (oturumun bağlamında
`holding_advance_tag_counter`; tek-tenant ifadesine asla — `TestAdvance_APeersPlaqueGoesThroughTheHoldingStatementOnly`).
`not_peer`'de `gather`'ın saha okuması **yapılmaz** (eş olmayanın plaketi okunmaz). Kararın okuduğu plaket
durumu: ilerletmenin kilitteki durumu, yoksa katı saha okumasınınki, yoksa çözümleyicininki. Bayat
çözümleyici durumu mutantı kırmızı (`TestHoldingAdvance_DecideSeesTheCurrentStatus`); ilerletme ile saha
okumasının kendi aralarındaki önceliği `TestHoldingAdvance_StatusAtTheAdvanceOutranksTheLaterRead` ile
pinli (denetim, üçüncü göz madde 4: plaket ilerletmeden sonra kayba çevrilir; sıra ters çevrilince
sayacı ilerletmiş dokunuş `sys:tag-not-active` ile reddedildi — kırmızı; ilk turda bu mutant yeşildi).
Kancalar (`recordHooks`: çözümlemeden sonra, ilerletmeden sonra, yazmadan önce, site satırının iki yazma
denemesi arasında) üretimde nil'dir; yarışlar zamanlamayla değil kancayla kurulur.

**`ctr_gap` — kullanıcının K-1i kararı (2026-10-14), migration `00045`.** İlk teslimde dönüşte boşluk
sütunu yoktu, holding dokunuşunda `tap:ctrGap` 0'dı ve `base:ctr-gap-review` hiç ateşlenmiyordu; bu
adlandırılmış sınır kullanıcıya soruldu ve kullanıcı genişletmeyi seçti. `00045` fonksiyonu
`TABLE(outcome text, tag_status text, ctr_gap integer)` ile yeniden kurar (DROP + CREATE — dönüş tipi
`CREATE OR REPLACE` ile değişmez; sahip, `REVOKE ALL … FROM PUBLIC` ve tek `EXECUTE` yeniden verilir;
ön koşul canlı dönüşün `00043`'ünkü olduğunu doğrular). `ctr_gap` `moved` CTE'sinin `RETURNING`'inde
`(t.last_ctr − v.old_ctr − 1)::integer` — tek-tenant ifadesinin birebir aynı hesabı, aynı ifadede;
`advanced` dışındaki sonuçlarda NULL; Go tarafı (`db.HoldingAdvance.CtrGap`) NULL'un `advanced`'la
birlikte gelmesini hata sayar. **Neden `00043`'ü yerinde değil:** `00043`'ün Down'ı site satırı varken
reddeder (tasarımı gereği) ve paylaşılan `tappa-m11-db` 362 site satırı taşıyordu — geri alınamayan
bir migration'ı yerinde düzeltmek, uygulanmış veritabanlarında eski gövdeyi bırakırdı. `00045`'in
Down'ı `00043`'ün iki sütunlu fonksiyonunu birebir geri kurar. Ölçüldü: KF@100, KM 150 → KM'de `flag`,
`base:ctr-gap-review`, `policy_context`'te `tap:ctrGap` 49; ardından KF 151 → `ok`, `tap:ctrGap` 0
(atlama holding dokunuşunda tüketildi — §13 madde 15). Mutantlar: `00045`'in ifadesinde `ctr_gap` hep 0
(atılabilir kapta) → `TestHoldingAdvance_KeepsReplayProtection` ve
`TestHoldingAdvance_CounterGapReachesTheEmployersReview` kırmızı; `advanceAtAPeer`'de `gap: 0` → aynı
uçtan uca test ve `TestAdvance_APeersPlaqueGoesThroughTheHoldingStatementOnly` kırmızı.

**Karar motoru.** `policy.Context.SameHolding` (motor hesaplamaz); #1 `… ∧ ¬SameHolding`; yeni #9
`sys:cross-tenant-qr-site-ip` (effect `review`), #10/#11 yetkilendirme guardrail'ları. **Değişmez
testi:** *"guardrail yalnız kısıtlayıcıdır (deny/ignore/redirect)"* değişmezine **adlandırılmış tek
istisna** (`reviewGuardrails`: bu guardrail, K-1g kararı); gevşetilemezlik özellik testine bu guardrail'ın
senaryosu eklendi (2 000 rastgele düşman belge — hiçbiri `allow`'a çeviremez). Panelin kural defterine
gerekçe cümlesi eklendi (`guardrailRules`).

**Marka ve rota.** Tap sayfası eşin plaketinde `tenant.Directory.PeerTapPage` ile (oturum bağlamında
`holding_tap_site`) eşin mekânını, accent'ini ve — VIES'li ise — logosunu çizer; duvarsız plakette
marka çizilmez (ADR 0023'ün kuralı). Sonuç ekranı logoyu kaydın kendi `holding_tap_site` satırından alır
(ek okuma yok). **ADR 0024 M11 notunun açık bıraktığı `p_uid` kaynağı:** logo rotasının **eş biçimi**
`/t/logo/{sha}?tag=<uid>` — uid 14 büyük harf hex değilse ya da `tag` iki kez verilirse okumaya gitmeden
tek 404; eş değil / eski özet / başka işletmenin özeti / VIES yok aynı bayt-aynı 404. `tag`'siz rota
değişmedi.

**K-35.** Canlı oturum + bekleyen onaylı aktivasyon + NFC: `holding_tap_site` (oturum bağlamı) eşin plaketi
diyorsa ve plaketin işletmesi davetinkinden farklıysa sayfa sıradan holding dokunuşudur; davetin kendi
plaketi (oturum eşin çalışanınınki olsa bile) aktivasyonu tamamlar. Aktivasyon yolunun önizlemesi
koşmaz (ADR 0026'nın testi değişmedi); davet çözülmezse aktivasyon akışı cevaplar.

**Devredilenler (adıyla, nedeniyle):** *"BEFORE kabul edip AFTER'ın KF tarafını atladığı satır yok"*'un
AFTER yarısı ve *"C'nin `site_punches`'ı 0"*, KF'nin `refused_tag_not_active` görünümü ve kayıp plaket
uyarı yüzeyi, ve K-1i'nin KF tarafı (`ctr_gap`'in `policy_context`'teki `tap:ctrGap`'ten izdüşüme
taşınması — §9) → **API-1c** (`holding_feed`, `site_punches` orada doğar; plan §5 API-1c kabulüne
yazıldı); `HoldingDirectory_ReturnsPeersNeverTheCaller` → **API-8** (`holding_directory`; plan §5 API-8
satırı ve kabulüne yazıldı); R4 tarayıcısının şema nitelikli `UPDATE public.tags SET last_ctr`
biçimi → **API-3** (plan §5'in satırı; yeni ifade zaten çok satırlıdır ve R4 satır yereldir — ADR 0028 §3).

**Denetimden sonra (2026-10-14 — güvenlik ONAY + iki DÜŞÜK, üçüncü göz RED: bir ORTA + yedi DÜŞÜK).**
Ölçümler atılabilir bir PostgreSQL 17 kabında; paylaşılan veritabanına yalnız `goose up` (`00045`).
- **ORTA-1 — Down yürüyen testlerin kilitleri.** `holdingLockForDowns` altı tabloyu kilitliyordu;
  `holding_members`, `holdings`, `operator_audit_log`, `operator_read_tickets` eklendi (on tablo, tek
  `LOCK … NOWAIT`, 25 ms'de bir yeniden, en çok 90 sn). Denetçinin sondası kendi kabımda yeniden
  koşuldu — `TestHolding00041_PreconditionRefusesAWrongCluster -count=10` sürerken ayrı bir bağlantı
  her işlemde bir üyelik yazıp siliyor: **altı tabloyla 1 361 üyelik yazımında 90 deadlock** (her
  Down yürüyüşü bir tane — 10 koşu × 9 savepoint; kurban hep döngü, test 110 sn), **on tabloyla 2 324
  yazımda 0** (39 sn). Liste artık **sürüklenemez:** `holdingInSavepoint` savepoint'i geri almadan önce
  bu arka ucun **güçlü** (ShareUpdateExclusive ve üstü) kilitlerini okur; listede olmayan bir tablo
  varsa test kırmızıdır (`strongTableLocksOutside`; mutant: `holding_members` listeden çıkarılınca
  tabloyu yeniden kuran üç test kırmızı — tabloyu düşürüp öyle bırakan bir savepoint'te kilit düşen
  OID'dedir ve adı okunamaz, denetim orada kördür). Hata beklenen ifadeler iç savepoint'te koşar (`holdingNestedRun`), yoksa kilitler
  okunamazdı. **Taranan öteki Down yürüyücüsü `opAtVersion`'dır** (M10'un operatör testleri; `00042`
  dahil): onun Down'ları `operator_audit_log`'u sonra `operator_read_tickets`'ı kilitliyordu, oysa
  `op_read_audit` önce bileti günceller sonra günlüğü okur — ters sıra, aynı sınıf. `opAtVersion` artık
  yürüyeceği Down'ların DDL'inin adlandırdığı tabloları (`downTables`) ve sildiği iki tabloyu önce, tek
  `NOWAIT` ifadesiyle alır ve aynı sürüklenme denetimini yapar (mutant: `downTables` boş →
  `employee_invites` (`00035`'in Down'ı) on testte kırmızı). Yorumdaki *"bir işlem asla beklemez"*
  iddiası daraltıldı: iddia yalnız listedeki tablolar içindir.
- **Güvenlik DÜŞÜK-1 — kapının `lock_timeout`'u.** §6 ve §13 madde 16: site satırı savepoint içinde,
  yalnız kapının 55P03'ünde bir kez yeniden yazılır. Kancayla kurulan yarış
  (`TestHoldingWindow_GateLockWaitIsWrittenOnceMore`): çıkarma üyelik satırını commit etmeden tutar,
  ilk INSERT 2 sn sonra 55P03, kanca çıkarmayı commit eder, ikinci deneme ilerletilmiş NFC satırını
  **yazar** (QR'da 23514 → `ErrSiteMembershipEnded`). Kalan yol `TestHoldingWindow_GateLockHeldThroughBothWaitsIsTheNamedCost`:
  iki bekleme (≥ 4 sn), 55P03, kayıt yok, KF sayacı 101. Mutant (yeniden deneme yok) → iki test kırmızı.
- **Güvenlik DÜŞÜK-2 — `HoldingSite`'ın JSON yolu.** K-39'un üç değeri artık alan değildir:
  dışa açılmayan bir **kapanışın** içindedir (`StaticIPs()`, `GPS()` erişimcileri). İşaretçi yetmedi
  (ölçüldü): dışa açılmayan bir alanın içindeki `HoldingSite`'ın `%s`/`%q`'su fmt'nin hatalı fiil
  yolunda işaretçiyi izleyip adresin tamsayısını basıyordu (sekiz yol). `MarshalJSON` eklendi (site +
  durum). `TestHoldingSite_EvidenceLeavesByNoRoute`: sekiz fmt fiili, bir hata, slog'un iki işleyicisi
  (öznitelik, grup, `With`), `json.Marshal` × on üç sarmalama (değer, işaretçi, dışa açık / açılmayan
  alan, dilim, harita) — kanarya (`198.51.100.77/32`, 35.111327/14.222391; noktalı, hex, netip'in
  tamsayıları, numeric'in rakamları) hiçbirinde yok. `LogValue`/`Format`/`MarshalJSON` artık kanıtı
  tutan şey değildir (üçü birden kaldırılınca kanarya sızmadı — ölçüldü); satırın geri kalanını
  (mekân adı, accent, logo) günlükten ve JSON'dan uzak tutarlar.
- **Üçüncü göz DÜŞÜK'leri:** (1) kapının QR satırı — yukarıda *"Kapı"*; (2) duvarsız eş plaketinde
  marka çizilmez — `TestHoldingBrand_TapAndResultShowTheSiteBrand`'in son yarısı (`PeerTapPage`'in
  `LocationID == nil` dönüşü çıkarılınca kırmızı); (3) `ErrSiteMembershipEnded` → 409 + *"Tap the
  plaque again"* — `TestCheckin_SiteMembershipEndedIs409TapAgain` (dal çıkarılınca 500, kırmızı);
  (4) durum önceliği — yukarıda *"Tap yolu"*; (5) plan §5'in API-1c kabulüne devredilen dört madde ve
  API-8 satırı/kabulü yazıldı; (6) K-1i ile çözüldü; (7) yarış testi gerçek sorguyu koşar — yukarıda.
- **(k) — indeks notu.** `00043`'ün kısmi indeksi (`transactions_site_idx`) boş da olsa yapılırken
  tabloyu **ACCESS EXCLUSIVE altında bir kez tarar**: O(n) — 688 000 satırda 0,12 sn, 2 000 000'de
  0,35 sn, doğrusal uzatımla 2 sn eşiği **~11 M satırda**. Üretimin `transactions` boyutu bu oturumdan
  ölçülemedi (üretim veritabanına erişim yok — `00043` uygulanmadan önce `SELECT count(*) FROM
  transactions` ile bakılmalı); el kitabının ölçeği (100+ kullanıcı, kişi başı günde ~2 dokunuş) yılda
  ~75 000 satırdır, yani eşik bu ölçekte on yıllar ötededir. Paylaşılan `tappa-m11-db`'de ~9 700 satır.

**Yeniden denetimden sonra (2026-10-14 — iki mercek ONAY; kalan DÜŞÜK'ler).** Ölçümler yeni bir
atılabilir PostgreSQL 17 kabında.
- **Üçüncü göz DÜŞÜK-A — kilit önce, mekanik olarak.** Kilit, Down yürüyen her gövdenin hatırlaması
  gereken bir çağrıydı (ölçülen mutant: `TestHolding00043And00044_DownGiveBackAndUpTakesItAgain`'in
  ilk savepoint'inden çağrı çıkarılınca test yeşil, yük altında 20 deadlock). Artık iki yardımcı var:
  `holdingLockedSavepoint` kilidi gövdeden **önce** kendisi alır ve sonunda listedeki her tablonun
  ACCESS EXCLUSIVE tutulduğunu doğrular; `holdingInSavepoint` güçlü tablo kilidi alan bir gövdeyi
  **reddeder**. Mutantlar kırmızı: o savepoint `holdingInSavepoint`'e çevrilince (*"güçlü kilit:
  locations, tags, transaction_reviews, transactions"*); yardımcının kilit çağrısı çıkarılınca üç
  test. Denetim bir gerçek bulgu da verdi: `TestHoldingCheck_ChannelRules` kapıyı `DISABLE TRIGGER`
  ile kapatırken `transactions`'ı önce hiçbir şey almadan ACCESS EXCLUSIVE kilitliyordu — artık
  `holdingLockedSavepoint`'te. (`strongTableLocksOutside`'a nil liste SQL NULL gidiyordu ve
  `<> ALL (NULL)` her kilidi gizliyordu — mutant yeşil kalınca bulundu, düzeltildi.)
- **Üçüncü göz DÜŞÜK-B — tek ifadeli NOWAIT'in açlığı.** Sonda: tablo başına beş oturumun kısa
  işlemleri (10 ms tut, 10 ms boş) + iki tut-ve-bekle oturumu (dokunuş: `tags` → `transactions`;
  fikstür: `locations` → `tags`); son koşuda ayrıca bir üyelik yazıcısı. **Eski yol (yalnız NOWAIT):** iki test
  90 sn'de 55P03 ile düştü (~3 000 tur). **Önerilen yedek — listenin tamamına engelleyici `LOCK`,
  `lock_timeout` 5 sn:** testler geçti ama yükte **3 deadlock kurbanı**, testin kendisi de bir kez — aldığı
  tabloları tutarak bir sonrakini beklemek ORTA-1'in sınıfıdır. **NOWAIT tek tek, aldığını tutarak:**
  yine 90 sn (yoklama tablonun kuyruğuna hiç girmez). **Uygulanan (`lockInTurn`):** 40 NOWAIT
  ıskasından sonra tablolar sırayla, her biri **engelleyici** ama **sınırlı** bekler — tek bekleme
  `deadlock_timeout`/5'i, yeni bir beklemenin başlangıcı ilk kilitten `deadlock_timeout`/2 sonrasını
  geçmez; böylece bizi bekleyen bir işlemin deadlock denetimi (beklemesinden `deadlock_timeout`
  sonra koşar) döngü bittikten sonra koşar, kendi denetimimiz hiç koşmaz. Süresi dolan tablo bir
  sonraki turda başa alınır (üyelik yazıcısı `holding_members`'ı tutup anahtar denetiminde
  `tenants`'ı bekliyor — `tenants` önce alındığında her tur `holding_members`'ta doluyordu, 90 sn;
  başa alınınca geçti). Üyelik yazıcısız yükte üç koşu 17–21 sn, yazıcılı yükte **6/6 test geçti
  (64 sn), yükte 0 deadlock, 2 815 üyelik yazımı**. Aritmetiğin kapsamadığı tek durum adıyla yazıldı: üçüncü üyesi ikinciyi bizden bağımsız
  bekleyen üçlü bir döngü (sonda üretmedi).
- **Güvenlik D-1 — `last_ctr` türetimi** K-1i'nin bedel sütununa ve §12 madde 3'e yazıldı.
- **Güvenlik D-2 — kilit beklemesinin tetikleyicileri** §13 madde 16'da, ölçümleriyle; kural
  `deploy/README.md`'de (*"Holding tablolarına dokunan migration — `lock_timeout` kuralı"*).
- **Üçüncü göz DÜŞÜK-C:** `insert`'in yorumu ölçümle uyumlu yazıldı (yeniden deneme 2–4 sn arası
  tutmaları kurtarır); `op_remove_holding_member` bir UPDATE ve en çok üç INSERT'tür (§13 madde 16,
  `NamedCost` testinin yorumu).
- **API-1c devri:** K-1i'nin KF tarafının tetikleyici gövdesinin `policy_context`'ten yalnız
  `tap:ctrGap`'i okuduğu bir gövde taramasıyla pinlenmesi plan §5'in API-1c kabulüne yazıldı.

## API-1c uygulama notu (2026-10-14)

Yapıcının uygulaması ve ölçümleri; ADR'nin kararları değişmedi — sapmalar ve ADR'nin açık bıraktığı
kararlar aşağıda adıyla. Yıkıcı ölçümler ve mutantlar atılabilir bir PostgreSQL 17 kabında
(`scripts/db-init` ile kurulu, `arwd` varsayılanı) koşuldu; paylaşılan `tappa-m11-db`'ye yalnız
`goose up` (`00046`, `00047`).

**İki migration.** `00046` `site_punches`'ı (RLS beşlisi, satır + TRUNCATE korumalı append-only,
`APPEND_ONLY`/`trunc_tables` listelerine sekizinci tablo olarak), `tappa_app`'in `sun_valid` hariç
sütun listeli SELECT'ini, tanımlayıcının yetkilerini ve `holding_feed`'i iki tetikleyiciyle
(`transactions_holding_feed`, `transaction_reviews_holding_feed`) kurar; `00047`
`holding_site_locations`'ı (yeni yetki yok — `00043`'ün sütunları yeter). `00046`'nın Down'ı
izdüşümde satır varken **reddeder** (`00043` emsali: sahanın gördüğü şey — tekilleştirmesi ve sunucu
saati — işverenin satırlarından yeniden kurulamaz). İkisi de operatör nesnesi adlandırmaz,
`opAtVersion` atlar; Down yürüyen testlerin kilit listesine (`holdingDownTables`) `site_punches`
eklendi ve `holdingBelow43`, `00047`/`00046`'yı önce yürür (`holdingBelow46`).

**Tablo — otonom eklemeler (sütun eklemez, §9'un listesi birebir):** FK'ler `(transaction_id,
employer_tenant_id) → transactions(id, tenant_id)`, `(site_location_id, tenant_id) → locations`,
`(site_tag_uid, tenant_id) → tags`, `tenant_id → tenants` (hepsi RESTRICT; lokasyon silmenin ikinci
kemeri); CHECK'ler `employer_tenant_id <> tenant_id` ve `bucket_hour` bir UTC saat başı (iki argümanlı
`date_trunc` mutantı bu CHECK'e çarpıp işverenin kaydını düşürürdü — ölçüldü; testi CHECK'i kaldırarak
koşar). İndeksler: `(tenant_id, created_at DESC)`, `(tenant_id, site_tag_uid, created_at DESC)`,
`(tenant_id, site_location_id)` ve §9'un iki kısmi tekil indeksi birebir.

**Eşleme — açık bırakılanların kararı.** `manual` × `ok`/`flag`/`reject` → `employer_manual`
(`ignored` yazılmaz); bir manuel `flag`'in onayı da `approved`/`rejected` olarak düşer (ürün manuel
`flag` yazmaz — `InsertManualTransaction` `ok` sabitler). `CASE` kapalı `verdict`/`channel` kümeleri
üzerinde eksiksizdir; bilinmeyen bir hücre hata değil boş dal olurdu (§4.6) — katalog testi
(`TestSitePunches_OutcomeMappingIsTotal`) her hücreyi dolaşır. Onay kolu `holding_peer
'strict_stmt'` sorar, **`lock` almaz** (otonom: bir onay üyelik satırında beklemez; eşzamanlı commit
edilmemiş bir `op_remove` ile kesişen onay sahaya düşebilir — onay anında eşlik sürüyordur).
Mutantlar (gövde `CREATE OR REPLACE` ile değiştirilip): `ignored` dalı yok + eşlenmemiş hücre hata →
işverenin kaydı yazılmadı (kırmızı); yalnız `ignored` dalı yok → manuel `ignored` sahaya
`employer_manual` düştü (kırmızı); `sys:tag-not-active` kolu yok → kayıp plaket `refused` (kırmızı).

**Ölçülenler.** (1) Hedefli `ON CONFLICT … WHERE … DO NOTHING` hedef sütunlarda **ve yüklemin
sütunlarında** SELECT ister: `sun_valid` SELECT'i geri alınınca 42501 — §3'ün yedi sütunu (yükleminkiler
dahil) bu yüzden. (2) `NULLS NOT DISTINCT`: çalışansız ve `sun_valid` NULL/false üç red → 1 satır.
(3) `date_trunc('hour', now(), 'UTC')` UTC, Malta ve Kolkata oturumlarında aynı an; iki argümanlı
biçim yalnız Kolkata'da 30 dk kaydı (§9'daki düzeltme). (4) Tekillik beklemesi 2,14 sn'de 55P03 (§13
madde 18 — ürün yolunda erişilemez). (5) 200 sahte URL tek ifadede → KF'de 1 satır, KM'de 200; mutant
(saat anahtarına `ctr`) → 200. (6) `outcome` anahtardan çıkarılınca sahte `refused` kayıp plaket
uyarısını yuttu (0) — gerçek indeksle 1.

**Tanımlayıcının yetkisi — her sütun ölçülerek** (`TestHoldingDefiner_EveryGrantIsUsed` artık bir
onay, iki red biçimi ve `holding_site_locations` da sürer): `site_punches` INSERT (18 sütun) ve SELECT
(7 — iki çakışma hedefi); `transactions` SELECT (`id, tenant_id, employee_id, ctr, type, occurred_at,
sun_valid, channel, site_tenant_id, site_location_id, site_tag_uid` — yalnız onay kolu okur;
`transactions` kolu `NEW`'i okur, yetki istemez); `employees` SELECT (`id, tenant_id, full_name,
external_ref, employee_code`). §3 matrisinin `employees.first_name/last_name/status/updated_at`
(API-8) ve `transactions.verdict/practice` (API-2) satırları verilmedi — matris üst sınırdır.

**K-1i'nin saha yarısı.** `ctr_gap` = `NEW.policy_context -> 'tap:ctrGap'` bir sayı, tam ve
0 ≤ x < 2^24 ise; değilse NULL — iç içe `IF` ile (SQL bir `AND`'in sırasını vaat etmez; bir jsonb
metninin cast'i hata verip işverenin kaydını düşürürdü). Gövde taraması: yorumları ayıklanmış
tanımda `policy_context` tam bir kez ve yalnız bu biçimde geçer; üç mutant (başka anahtar, bağlamın
tamamı, `->>`) işaretlenir. Uçtan uca: KF@100, KM 150 → KF'nin satırı `pending_review`, `ctr_gap` 49.

**K-33 — tek yüklem `holding_feed`'de.** `transactions` kolu eşliği sormaz (§6); mutantların
ulaştığı kol onayınkidir: KM'nin KF'deki `flag` satırı eşlik sürerken yazılır, KM H'den çıkıp H2'ye
katılır, KM'nin yöneticisi onaylar — gerçek yüklemle KF'ye onay düşmez, M1/M2/M3'ün her biriyle düşer
(`holdingFeedProbes`). `holding_site_locations` için C/D bağlamı KF/KM mekânı, KM bağlamı C/D mekânı,
bağlamsız çağrı herhangi bir mekân okursa kırmızı; üç mutantın üçü de kırmızı. Sonda **bütün
sayfaları** okur: ilk biçim yalnız ilk sayfaya bakıyordu ve paylaşılan veritabanında M1'in sızıntısı
(her tenant'ın her mekânı, kimliğe göre 1000'er) sonraki bir sayfadaydı — M1 yeşil kaldı (ölçüldü,
tam koşuda).

**"BEFORE kabul edip AFTER KF tarafını atladı" satırı yok.** `TestHoldingWindow_NoRowPassesBeforeAndSkipsAfter`'a
iki alt test: commit edilmemiş bir çıkarmayı bekleyip toleranslı kolla kabul edilen ilerletilmiş NFC
satırı aynı işlemde KF'nin izdüşümünde (mutant: `transactions` koluna `strict_stmt` sorusu eklenince
KM'de 1, KF'de 0); anahtar beklemesinden sonra commit edilen satır KF'de.

**İşverenin dokuz okuyucusu** (`internal/domain/ledger/site.go`): `holding_site_locations` işlem başına
**bir kez** ve yalnız sayfada `site_*` satırı varken okunur (holding verisi olmayan işletme
`holding_site_locations`'ı çağırmaz; ama sahanın üç izdüşüm okuması — rapor, uyarı, boşluklar — her
tenant için koşar, son görülme de plaket listesinde — ve sayfaları, denetimden sonra, bayt bayt
aynıdır: aşağıda E-D3). `ListPanelTransactions`/`ListFlaggedForReview`
eşin mekânını ve plaketini adlandırır, güven puanını ve IP/GPS işaretlerini **boşaltır** (K-39 —
§13 madde 5'in *"panel göstermez"*i; fiş *"not shown at a partner venue"* der); `ListOpenCheckIns`
adlandırır; `ListWorkedShiftEvents` + `resolveShift` eşin mekânının kovası ve vardiyası (geç kalma,
departman vardiyası yoksa tap edilen eş mekânın vardiyasıyla — K-36 aynı saat dilimini garanti
eder); `ListAnomalyVenues` eş mekânı ayrı satır olarak sayar, iki kanıt rakamını (`distance_only`,
`outside_venue`) yalnız kendi mekânlarında sayar (K-39); `ListTapsTakenTogether` artık eş mekânı
dahil eder; `ListAnomalyPlaques` ve `ListTagLastSeen` işverende eşin plaketini **dışarıda** tutar
(sahaya kendi izdüşümünden gelir); `CountLocationReferences` sahada izdüşümden sayar.
`TestEmployerReaders_NineReadersNameTheSite` dokuzunu iki yönde ayrı vakalarla sürer; Go tarafı
mutantları (ad yok, K-39 boşaltma yok, eş vardiyası çözülmez, çift adsız) ve SQL mutantı (K-39
filtresi yok) ilgili vakada kırmızı.

**Sahanın paneli.** Rapor: haftanın eş-personel dokunuşları `transaction_id` başına **son** sonuçla
(`DISTINCT ON`, onay/ret aynı işlemdeki `pending_review`'u geçer), `occurred_at` penceresiyle (raporun
kendi penceresi), süre yok (*"Hours: at their employer"*), `employer_manual` ayrı başlıkta ve hiçbir
toplamda değil; `pending_review` her zaman *"Result at their employer"*. Anomaliler: kayıp plaket
uyarısı (sunucu saati — `created_at` penceresi ve zamanı), sayaç boşlukları (izdüşümün `ctr_gap`'i,
plaketin kendi satırına eklenir). Plaket listesi: *"son görülme"* kendi dokunuşunun ve izdüşümün
`created_at`'inin geç olanı. Lokasyon silme: *"N taps by a partner business's staff"*; yalnız
`ignored` bir eş satırı izdüşümde yoktur, sayım 0'dır ve FK RESTRICT silmeyi yine reddeder
(`ErrVenueInUse`). KF onay veremez: inceleme ifadesi satırını inceleyenin tenant'ındaki bir `flag`
kayıttan kurar — KM'nin kaydı 0 satır (`TestSitePanel_CannotReview`).

**Manuel kayıt** (`InsertManualSiteTransaction`): form eşlerin mekânlarını ayrı, isteğe bağlı bir
seçimde sunar (holding'de olmayan işletmede çizilmez); seçim MAC'lı onayın konusuna girer
(`/site/<id>`); alan adı `domain`'de **aynı işlemde** `holding_site_locations` ile yeniden çözülür
(eş değil, bilinmeyen ya da üyeliği bitmiş → `ErrVenue`) ve kapının 23514'ü `ErrVenue`'ya çevrilir;
satır işverenin, `location_id`/`tag_uid` NULL, `department_id` çalışanınki; audit ayrıntısına
`partner_venue: true` (yalnız o kayıtlarda). Kapının 23514 dalı ürün yolundan test edilmedi (alan adı
okuması aynı işlemde önce düşürür; yarış kancası yok) — veritabanı yarısı
`TestHoldingWindow_ManualAfterLeftIsRefused`.

**Saklama.** `site_punches` süresiz tutulur; süre kullanıcının DPA/aydınlatma notuyla belirlenecek
(§13 madde 4). Append-only olduğu için bir silme yolu yazılmadı.

**Denetimden sonra (2026-10-14 — iki mercek RED: güvenlik bir ORTA + bir DÜŞÜK, üçüncü göz bir ORTA +
dört DÜŞÜK).**
- **G-ORTA-1 (K-39, not):** işverenin fişi ve onay kuyruğu eş mekân satırında motorun notunu
  basıyordu — *"verified via GPS only — no network proof of place"*, *"network proof of place: the
  source IP matches the location"*, *"neither IP nor GPS could place this tap"*, çapraz QR
  guardrail'ının *"needs that venue's own network"* cümlesi: gizlenen bitlerin düz metni. Artık
  `nameSiteRecords` böyle bir satırın notunu `PartnerVenueNote` ile değiştirir (*"Decided by your
  rules at a partner venue — evidence is not shown."*), katmana bakmadan: tenant'ın kendi kuralı
  sevk edilmiş bir ifadenin kopyasıdır, gerekçesi dahil (`TestCopyOfShipped_KeepsTheRulesAndChangesOnlyTheScopeAndTheSid`),
  yani aynı cümle `policy_layer = 'tenant'` ile de gelir. `NoteIsTenants` yine kimin kuralı olduğunu
  söyler. Test: `TestEmployerReaders_APartnerVenueRowCarriesNoEvidenceSentence` (dört kanarya, gün
  listesi ve kuyruk; kontrol: kendi mekânındaki satırın notu aynen) ve panel HTML'inde
  `TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason`. Mutant (değiştirme yok) → 12 kanarya
  sızıntısı, kırmızı.
- **E-ORTA-1 (§4.6, izdüşümün kilit beklemesi):** §13 madde 18 — checkin'in tek yeniden denemesi
  `holding_feed`'in 55P03'üne genişledi; sınama sahanın `tenants` satırını `FOR UPDATE` tutarak
  (yalnız o testin tenant'ı — bir tablo kilidi paralel paketleri bekletirdi); tablo kilidi biçimi
  atılabilir kapta ölçüldü. Onay kolu adlandırıldı.
- **E-D1:** onay kolunun katılığı 60 sn penceresinde pinli
  (`TestHoldingFeed_ApprovalInsideTheExitWindowIsNotProjected`; mutant `tolerant_stmt` → KF'ye düştü,
  kırmızı); §13 madde 7 *"commit edilmiş bitiş"* diye nitelendi.
- **E-D2:** §13 madde 21 ve backlog T124 — bu turda migration yok.
- **E-D3:** holding'siz işletmenin rapor ve anomali sayfası bir bayt (kardeş bileşen çağrısının
  boşluğu) farklıydı. Yeni bloklar `templ.Join` ile önceki bileşene bağlandı; `85b4b8d`'deki şablonla
  karşılaştırma (aynı görünüm, iki sürüm): rapor, anomali, fiş ve manuel form (iki adım) bayt bayt
  aynı.
- **E-D4:** `ListSitePlaqueGaps`'in `records`'u onay/ret satırlarını da sayıyordu; artık dokunuşları
  sayar (`TestSitePanel_PlaqueGapCountsTheTapOnce`; mutant `count(*)` → 2, kırmızı).
- **G-D1:** §9 ve §12'de *"sütun olarak"*; §13 madde 22.

**İkinci denetimden sonra (2026-10-14 — iki mercek RED: bir ORTA her birinde, DÜŞÜK'ler).**
- **ORTA-A (güvenlik, K-39 — anomali ekranı):** `CountAnomalySignals` ve `ListAnomalyPeople`'ın kanıt
  sayıları (judged, distance_only, outside_venue, outside_venue_on_network) artık yalnız kendi
  mekânlarını sayar; eş satırları `partner_records` / `partner_answerable` olarak ayrı sayılır
  (`AnswerableOwn` outside_venue'nun tabanı) ve toplam kartı bir cümleyle söyler (eş satırı yoksa
  çizilmez). `ListAnomalyPolicySids` eş satırlarını işaretler, ledger hepsini adsız tek kovaya toplar.
  Kanıt olmayan sayaçlar (sayaç boşluğu — K-1i —, farklı şube, eğitim, elle yazılan, snapshot'sız)
  eş satırlarını saymaya devam eder. Test: `TestAnomalies_APartnerVenueRowAddsNoEvidenceFigure`;
  mutantlar (toplamın judged'ı, kişi sorgusunun HAVING'i, kural katlaması) kırmızı. **Envanter:**
  `TestEvidenceReaders_EveryOneIsClassified` — `db/queries`'te `transactions` ve bir kanıt/karar sütunu
  okuyan her ifade (15) sınıflandırılmış ve sınıfının dayandığı parçayı taşır; envanterin
  tanıdığı sütun listesini okuyan yeni bir okuyucu kırmızıdır (kontrol: eklenen bir `ip_match` okuyucusu
  yakalandı) — kapsamı üçüncü denetimden sonra genişledi, aşağıda. Go'da elle yazılmış bir
  `transactions` okuyucusu yok; dışa aktarım (`ListWorkedShiftEvents`) ve onay sayısı (`CountPendingFlagged`)
  yalnız verdict okur — madde 5 (a).
- **ORTA-B (üçüncü göz — not değiştirmesi fazla genişti):** değiştirme artık yalnız kanıt anlatan
  kuralların notunu hedefler (kapalı küme, sid'e göre — §9 K-39), manuel kanal muaf, yön notu
  (`tap.StaleOpenInNote`, `appendNote`'un `"; "` eki) korunur. Kayıp plaket nedeni, sayaç boşluğu,
  farklı şube notu ve manuel kaydın yöneticinin notu görünür. `gps-conflict-review` kümeye
  eklendi (kapsamın dışında listelenmişti: cihazın eşin yarıçapı dışında olduğunu söyler). Test:
  `TestEmployerReaders_APartnerVenueRowCarriesNoEvidenceSentence` (12 vaka, gün listesi ve kuyruk);
  mutantlar — kanal muafiyeti yok, küme daraltıldı, genişletildi, `own:` düştü, yön notu düştü — beşi
  de kırmızı.
- **Kilit belgesi:** madde 18'e KF `tags`/`locations` satırlarının mekanizması ve sınırı;
  `deploy/README.md`'nin el işleri listesine `tags`/`locations` üzerinde `FOR UPDATE`,
  `site_punches`'ta `CONCURRENTLY` olmayan `CREATE INDEX` / `REINDEX`.
- **D-2:** madde 5 (b). **D-3:** holding'siz rapor ve anomali sayfasının bayt eşliği pinli
  (`TestHoldinglessPanels_AreByteIdenticalToTheGolden`, altın dosyalar `85b4b8d`'nin koduyla yazıldı;
  mutant: kardeş bileşen çağrısına dönüş → 1 bayt, kırmızı).

**Üçüncü denetimden sonra (2026-10-14 — güvenlik ONAY, üçüncü göz RED: bir ORTA + yedi DÜŞÜK).**
- **ORTA (kovanın yeri kuralı söylüyordu):** kova, eş satırlarını kendi kural satırlarına katlayıp
  sorgunun `ORDER BY count(*)` sırasında duruyordu — kovanın konumu eşin kuralının kaç kayıt aldığını,
  bir kural satırının eksilmesi hangi kural olduğunu ele veriyordu; eş satırları da sorgunun 50'lik
  `LIMIT`'ini tüketiyordu. Şimdi `ListAnomalyPolicySids` eş satırlarını `site_tenant_id IS NULL` ile
  dışlar (eş sütunu kalktı, sıra `count DESC, sid, katman` — belirleyici), ledger kovayı sayısını
  `Totals.Partner`'dan alarak **her zaman sona** ekler. Test: `TestAnomalies_ThePartnerBucketSaysNothingByItsPlace`
  (eşin iki farklı kuralı → kural listesi aynı; 60 farklı kurallı eş satırı 3 kendi kuralının yanında
  `LIMIT`'i tüketmez — liste kısalmaz, kova son ve 60); mutantlar — süzgeç kalktı, kova başa, kova sayısı sorgudan — üçü de kırmızı.
- **D-1:** madde 5 (e) (kural önceliği artığı) ve (c)'nin ifadesi.
- **D-2:** yeşil kalan SQL mutantları için vakalar (`TestAnomalies_EveryEvidenceFigureLeavesThePartnerOut`:
  IP evet + çakışma, kişi ve mekân sırası, eşte manuel satır); yedi mutantın yedisi kırmızı.
- **D-3:** envanterin sütun listesi `source_ip`, `gps_lat`, `gps_lng`, `gps_distance_m`, `sun_valid`, `ctr`
  ve bütün satır okumalarıyla (`SELECT *`, `t.*`, `RETURNING *`, `sqlc.embed`) genişledi; `must` artık
  süzgecin **kaç** rakamda durduğunu sayar (bir rakamdan düşürülen süzgeç kırmızı). Neyi görmediği
  testin başında yazılı (db/queries dışındaki SQL, başka adla erişen okuyucu, süzgecin doğru rakamda
  olup olmadığı — o yarıyı SQL mutantları taşır).
- **D-4:** holding'siz eşlik handler katmanında da pinli (`TestHoldinglessViews_AreByteIdenticalToTheGolden`:
  domain ekranı → `fillReportsView` / `fillAnomaliesView` → sayfa; altın dosyalar `85b4b8d`'nin koduyla
  yazıldı); mutant `partnerLine`'da `n <= 0` → `n < 0` kırmızı.
- **D-5:** madde 16'nın *"çıkarım, ölçülmedi"* cümlesi denetimin ölçümüyle değişti.
- **D-6:** `Record.matchedSid`'in yorumu düzeldi (her satıra yazılır, yalnız eş satırında okunur);
  `Record` hiçbir yerde loglanmaz/kodlanmaz, sid kanıt değildir — koruyucu metod eklenmedi.
- **Kapsam dışı gözlem:** Q16'nın *"IP iste"* kolu (`base:gps-only-allow`'u kapatmak) başka şubedeki
  GPS-only dokunuşu `base:cross-location-note` üzerinden `ok` bırakıyor — ölçüldü
  (`TestBaseline_RequiringIPLeavesACrossLocationGPSTapAllowed`), backlog T125; kod değişmedi.

**Dördüncü turdan sonra (2026-10-14 — iki mercek ONAY; kalan DÜŞÜK'ler, yeniden denetimsiz).**
- **D-a:** holding'siz işletmenin anomali sayfasında *"partner business"* yok (`TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason`'ın
  kontrolü); mutant `Partner >= 0` (boş haftada 0 kayıtlı kova) kırmızı.
- **D-b, D-c, D-d:** `TestAnomalies_ThePolicyListIsCutInSQLAndItsOrderIsTotal` — 52 kendi kuralı ve 3 eş
  satırı: liste sorguda kesilir (50 satır + *"more"*), son satır 3'lük kova. Aynı sid iki katmanda
  (NULL ve guardrail): NULL önce. Mutantlar — kova `Records − Σ listelenen` (5 verdi), okuma sınırı bir
  eksik (*"more"* yok), `policy_layer NULLS FIRST` kalktı — üçü de kırmızı. Üçüncü turdaki *"kova sayısı
  sorgudan → kırmızı"* cümlesi bu vakayla doğrulandı.
- **D-e:** envanter çıplak adı da yakalar (`readsWholeRow`: `SELECT t`, `to_jsonb(t)`, `row_to_json(t)`)
  ve `*`'ın her SELECT biçimini (`ALL`, `DISTINCT ON (…)`). Mutantlar — çıplak ad denetimi kalktı (4
  sonda kırmızı), eski `SELECT *` biçimi (`ALL *`, `DISTINCT ON … *` sondaları kırmızı).
- **D-f:** madde 16'nın ölçümü üç şekle ayrıldı (`tags` ilerletmeden önce/sonra, `locations`).
- **D-g:** madde 5 (e) değerlendiricinin gerçek sırasıyla yeniden yazıldı; review etkili kanıt dışı kuralın
  (`ctr-gap-review`) söylediği eklendi.


## API-8 uygulama notu (2026-10-15)

Yapıcının uygulaması; kararlar değişmedi — holding tarafının özeti (uç ve ölçümlerin tamamı ADR 0028 *"API-8 uygulama
notu"*nda). **Migration `00055`** (`db/migrations/00055_list_a_peers_staff.sql`: fonksiyon, dört sütun, kapı) ve **`00056`**
(`employees_tenant_updated_idx` — kendi dosyasında, çünkü Down'ı `employees`'e ACCESS EXCLUSIVE alır ve holding testlerinin
Down yürüyüşleri `00055`'in Down'ını koşar: ayrı tutulunca o yürüyüşler `employees`'i hiç kilitlemez).

- **`holding_directory(p_updated_since, p_cursor)`** imza tablosunun satırıyla birebir (katalog testinin `holdingSignatures`'ı;
  postkoşul da metni sınar): sahibi `tappa_holdingdefiner`, SECURITY DEFINER, `proconfig` yalnız `search_path` (§2 (v) —
  çağrılabilir fonksiyonlarda `lock_timeout` yok; okumanın süre sınırı API okumalarının `statement_timeout`'udur, 5 sn),
  `EXECUTE` tam olarak {sahip, `tappa_app`}, PUBLIC'ten REVOKE. Gövde: eş kümesi **bir kez** (`tenants` × `holding_peer(…,
  'strict')`, materyalize CTE) ve önünde üyelik kapısı (§2, T124); eş kümesi ayrıca çağıranın kendi id'sini dışlar
  (`s.id <> c.id` — `holding_peer(a, a)` yanlışken bile ikinci kemer, `TestHoldingDirectory_TheCallerIsKeptOutTwice` iki
  katmanı ayrı ayrı ölçer); veri satırları eş başına LATERAL `ORDER BY updated_at, id LIMIT 1000`, konum LATERAL'ın
  **içinde** (dışında olsa eşin ilk 1000 satırı imleçten sonrakileri iterdi — mutantı `TestHoldingDirectory_IsAKeysetOnUpdatedAtAndID`'de
  kırmızı); dış sıra `(updated_at, id)`, en çok 1000. Sayfa anahtarı iki argümandır: `p_cursor` NULL → `updated_at >=
  p_updated_since`; dolu → `(updated_at, id) > (p_updated_since, p_cursor)`. `active` = `status <> 'deactivated'` (K-1h;
  GET /v1/employees ile aynı). E-posta, `full_name`, ev lokasyonu yok.
- **Matris (§3):** `employees` satırının kalan dört sütunu verildi — `first_name`, `last_name`, `status`, `updated_at`
  (`TestHoldingDefiner_ColumnMatrix`, `TestHoldingDefiner_EveryGrantIsUsed` — sürüş `holding_directory`'yi holding'siz bir
  işletmenin bağlamında çağırır: kapı eş kümesini yürümeden hayır der, yürütücü ise ifadenin adlandırdığı her sütunu
  başlarken denetler — dört sütunun her biri tek başına geri alınınca 42501; çağrı kaldırılınca dördü de *"a grant nothing
  reads"* ile kırmızı, ölçüldü). `employees.email` verilmedi (*"asla SELECT"*; ön/son koşul da sınar).
- **Kanarya:** fikstürde eşin e-postası iğneleri taşıyan bir çalışanı var; `holding_directory` o satırı döner, hiçbir hücrede
  iğne yok (`TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks`). **Eş kümesi bir kez:** `holdingMultiRowCalls`'ta
  (`TestHoldingFunctions_PeerSetComputedOnce`).
- **K-33, H2 ile:** `holdingOutsiderProbes` artık `holding_directory`'yi de yürür (C, D, KM, bağlamsız; dünyanın ilk
  çalışanının `updated_at`'ından başlayarak, yasaklı bir satır görünene ya da sayfa kısalana dek). İki çok satırlı probun
  yürüyüşü ilk sızıntıda durur ve ilk sızan bağlamdan sonra öteki bağlamlar sorulmaz (gerçek süzgeç sızdırmaz, hepsini
  yürür): testlerin doldurduğu paylaşılan veritabanında (45 000 tenant) yanlış süzgecin her sayfası bütün tenant'ları yürür
  ve mutant testi 3 dk sınırını aşıyordu — ölçüldü: 180 sn (zaman aşımı) → 37,6 sn. M1/M2/M3 her biri fonksiyonda kırmızı (`TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers`).
  **Not:** mutant gövdeleri `is_member`'e artık gerçek cevabı verir — iki çok satırlı fonksiyon onu kapı olarak sorar ve
  süzgecin mutantı aynı zamanda üyelik sorusunu bozsaydı kapı onu gizlerdi (ölçüldü: eski harness'le iki fonksiyonun
  probu yeşil kaldı); mutantlar K-33'ün yanlış süzgeçleridir, bozuk kapı değil. Bozuk bir kapı kapalı yönde düşer (0 satır).
- **`holding_site_locations`'ın gövdesi** (`00047`) aynı migration'da kapıyla değişti (`CREATE OR REPLACE` — sahip, imza,
  ACL aynı); ön koşul eski gövdenin md5'ini (`63bf5ecf…`) ister, Down onu bayt bayt geri verir
  (`TestHolding00055_DownGivesBackAndUpTakesItAgain`). Down zincirleri: `feedBelow48` önce `00055`'in Down'ını, `feedUpAgain`
  sonda Up'ını koşar (tablo kilidi yok; `holdingDownTables` değişmedi); `opAtVersionSkips`'e 55 ve 56. *(İlk hâlde indeks
  `00055`'teydi ve `employees` `holdingDownTables`'a girmişti; tam `-race` koşusunda `TestHolding00041_PreconditionRefusesAWrongCluster`
  180 sn'de düştü — en sıcak tablonun ACCESS EXCLUSIVE'i Down yürüyüşlerine eklenmişti. Bölündü.)*
- **Yazılan testler:** `TestHoldingDirectory_ReturnsPeersNeverTheCaller` · `TestHoldingDirectory_IsAKeysetOnUpdatedAtAndID` ·
  `TestHoldingDirectory_TheCallerIsKeptOutTwice` · `TestHoldingFunctions_AMemberOfNoHoldingAsksOnce` (kapı; mutant —
  kapı iki gövdeden birinden kaldırılınca kırmızı) · `TestHolding00055_DownGivesBackAndUpTakesItAgain` ·
  `TestEmployees00056_TheDirectoryIndexGoesBack` · `TestEmployees00056_GivesUpAfterASecondBehindAnOpenWrite`.

