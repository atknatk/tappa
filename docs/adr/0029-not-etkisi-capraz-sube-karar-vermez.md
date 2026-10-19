# ADR 0029 — `note` etkisi: başka şubedeki dokunuş yalnız not alır, karar vermez

- **Durum:** Kabul edildi (2026-10-18)
- **Karar veren:** kullanıcı (2026-10-18: *"`base:cross-location-note` not-only olur"*)
- **İlgili:** ADR 0004 (§2 effect listesi, §7 beraberlik, §8 kapalı listeler), backlog T125,
  open-questions Q16 / Q17, CLAUDE.md §5 (satır 6–7, *"Farklı şubede tap nota işlenir,
  raporda ayrı görünür"*), §4.6

## Bağlam

`base:cross-location-note` (Q17) M3-06'dan beri **`allow`** etkiliydi: *"başka şubede
dokunmak normaldir, yalnız bu yüzden cezalandırılmaz"* demek için. Kuralın yorumu bunun
*"asla delik açmayacağını"* söylüyordu, çünkü daha kısıtlayıcı bir baseline ifadesi
koşulu tutunca kazanır.

Bu yalnız **her kanıt kuralı açıkken** doğruydu. Q16'nın *"IP iste"* kolu
`base:gps-only-allow`'u **kapatmaktır**; o zaman (backlog T125, ölçüldü — ölçüm testi bu
ADR'de ters iddiaya döndü ve adı `TestBaseline_RequiringIPHoldsACrossLocationGPSTapForReview`
oldu):

| Dokunuş (NFC, yalnız GPS) | Eşleşen | Sonuç |
|---|---|---|
| ev şubesinde | hiçbiri | `review` (varsayılan) |
| başka şubede | `base:cross-location-note` (`allow`) | **`ok`** |

Yani kolun kapatmak için var olduğu şekil, başka şubede açık kalıyordu. Aynı sınıfın
iki kardeşi de bu ADR'nin ölçümünde çıktı (aşağıda envanter): `base:ip-or-gps-ok`
kapalıyken başka şubedeki IP'li dokunuş, `base:no-evidence-review` kapalıyken başka
şubedeki **kanıtsız** dokunuş da bu `allow`'la `ok` oluyordu — sonuncusu bir sessiz onaydı
(§4.6).

## Karar

1. **Altıncı etki: `note`.** `internal/policy` effect listesine `note` eklenir. Belgede
   (baseline **ve** tenant) geçerlidir; **karar vermez**: değerlendiricide eşleşen bir
   `note` ifadesi kazanan seçimine **hiç girmez** (özgüllük, kısıtlayıcılık, belge sırası —
   üçüne de), yalnız `Reason`'ı `Decision.Notes`'a eklenir (sabit yürüyüş sırası, aynı
   cümle bir kez). Guardrail kararında not yoktur (guardrail terminaldir). Varsayılan
   kararda (`default`) not yine taşınır.
2. **Sınırlar (doğrulayıcı):** `note` yalnız `tap:record` eyleminde ve **boş olmayan**
   `reason` ile kabul edilir. Yetki eylemindeki bir not *"kim ne yapabilir"* hakkında bir
   şey söylüyormuş gibi görünüp hiçbir şey yapmaz; sözsüz not kayda bir şey eklemez —
   ikisi de ADR 0004 §8'in *"yok sayan motor en tehlikeli başarısızlıktır"* sınıfı.
3. **`base:cross-location-note` → `note`.** Başka şubedeki dokunuşun sonucu, **aynı
   kanıtla ev şubesindeki dokunuşun sonucudur** + kayıtta not. Kararı satır 6–7 ve
   tenant'ın kendi kuralları verir. Not, kaydın notunda kuralın cümlesinden sonra, yön
   notundan önce durur: `"<kural>; <başka şube>; <yön>"` (`tap.Decide`).
4. **Rapor değişmez.** Raporun *"ayrı görünür"* yarısı nota değil olguya bağlıdır:
   `employee:crossLocation` `policy_context`'e donar (M3-07), anomali sorguları onu okur.
   Not kapatılsa da olgu yazılır.
5. **`BaselineVersion` = `2026-10-18`** (ilk yükseltme). Duyuru metni
   `policy.BaselineChanges()`'tadır; panelin kurallar ekranı (`tenant.Rule.Changes` →
   `handler.policyRuleView`) bir kuralda *"daha yeni sürüm çıktı"* cümlesini **yalnız o
   kuralı değiştiren bir duyuru varsa** basar ve **ne değiştiğini** söyler. Damga tüm
   baseline için tek tarihtir; aksi hâlde yükseltme dokuz kurala birden *"güncel değil"*
   yazardı, oysa değişen biri. Kabul adımı (M7-03'ün *surface → accept → append v2*'si)
   hâlâ yok; cümle bu yüzden *"kabul edene kadar"* demez, **var olan hamleyi** söyler:
   kaydedilmiş kuralı kapatmak bugün yeni sonucu verir (karar eşdeğer; yalnız not düşer).
6. **İçerik damgaya bağlandı.** `TestBaseline_ContentIsPinnedToItsStamp` sevk edilen
   belgelerin baytlarını (damga hariç) damga başına sha256 ile pinler; damgayı yükseltmeden
   bir etki/koşul/gerekçe değişikliği, ya da duyurusuz bir yükseltme
   (`TestBaseline_EveryBumpIsAnnounced`) kırmızıdır. `baseline.go`'nun T23 notunun
   *"bunu mekanik olarak gören bir kapı yok"* dediği boşluk budur.
7. **Holding eşi (ADR 0027 K-39).** `ledger.partnerNote` bir kanıt kuralının cümlesini
   *"partner venue"* cümlesiyle değiştirirken motorun eklediği notları (politika notları +
   yön notu) korur — *"farklı şube"* işverenin bilmesi gerekendir ve kanıt anlatmaz.

## Değerlendirilen alternatifler

- **Koşulu daraltmak** (`crossLocation ∧ (ipMatch ∨ gpsMatch)` → `allow`; backlog'un ilk
  önerisi). **Kapatmaz:** `gps-only-allow` kapalıyken başka şubedeki GPS-only dokunuş bu
  koşulu yine tutar ve yine `ok` olur. Kanıt kuralını ikinci kez, kapatılamayan bir
  kopyayla yazmak olurdu.
- **İfadeyi baseline'dan silmek.** Not (§5 *"nota işlenir"*) ve tenant'ın onu tek
  başına kapatma anahtarı kaybolur; mevcut tenant'ların saklı `allow`'u yine olduğu yerde
  kalır (silmek de bir yükseltmedir).
- **`review` yapmak.** Başka şubede her dokunuşu kuyruğa atar — Q17'nin *"cezalandırma"*
  dediği şeyin ta kendisi.
- **Ayrı bir `Statement.Annotate` bayrağı.** Aynı anlamı effect'in yanında ikinci bir
  alanla taşır; `effect: allow, annotate: true` gibi çelişkili bir belge yazılabilir hâle
  gelir. Etki tek alan kaldı.

## Sonuçlar

**Davranış envanteri** (`TestBaseline_NoteOnlyChangesNoOutcomeUnderTheDefaultBaseline`,
128 girdi şekli × diğer yedi kanıt/inceleme belgesinin 2^7 aç/kapa birleşimi):

| Tenant yapılandırması | Değişen sonuç |
|---|---|
| varsayılan baseline (hiçbir şey kapatılmamış) | **0 / 128** — etki, sid, katman, gerekçe aynı; yalnız kayıt notuna cümle eklenir |
| yalnız `gps-only-allow` kapalı | 1 / 128 — başka şube, NFC, yalnız GPS: `ok` → `flag` (T125) |
| yalnız `ip-or-gps-ok` kapalı | 4 / 128 — başka şube, IP'li (NFC/QR, GPS'li/GPS'siz): `ok` → `flag` |
| yalnız `no-evidence-review` kapalı | 1 / 128 — başka şube, kanıtsız NFC: `ok` → `flag` (sessiz onay kapandı) |
| bu üçünden birini içermeyen her birleşim | 0 |

Her farkta eski sonuç `allow` via `base:cross-location-note`, yenisi `review`; değişiklik
yalnız **sıkılaştırır** ve yalnız başka şubede.

- **Mevcut tenant'lar değişmez.** Saklı v1 (`2026-07-26`, `allow`) duyuru kabul edilene —
  bugün: kural kapatılana — kadar karar vermeye devam eder
  (`TestPolicySetDB_RequireIPAndTheCrossLocationNote`). Geçmiş kayıtlar (§4.3) kendi
  `policy_version_id`'leriyle aynı gerekçeyi göstermeye devam eder.
- **Yeni tenant'lar** yeni belgeyi alır (ilk-ihtiyaç materyalizasyonu ve M7-03).
- **Tenant'ın eski kopyaları.** Panelin *"kendi kuralını yaz"* formu sevk edilmiş bir
  belgenin kopyasını üretir; 2026-10-18'den önce `cross-location note`'tan kopyalanmış bir
  `own:cross-location-note` **`allow` olarak kalır** ve bir mekâna (`location/<id>`)
  bağlıysa orada `location/*` incelemelerini **özgüllükle yener** — o mekânda başka şubeden
  gelen kanıtsız bir dokunuş `ok` olur. Bu ADR bunu kapatmaz (saklı belge değişmez, §4.3
  gerekçesi ADR 0004 §9); prod'da ölçülmesi gerekir. Bundan sonraki kopyalar `note`'tur.
- Panel her `note` ifadesini *"Adds a note to the record. It never decides the outcome."*
  diye çevirir.
- `checkin`'in *"tenant baseline'ı sevk edilenden eski"* INFO satırı artık mevcut her
  tenant'ı kapsar — tasarlanmış durum (M7-03 kabul akışı gelene kadar). *(2026-10-19:
  süreç başına tenant başına bir kez — aşağıdaki not, madde 4.)*
- **Geri dönüş (bu sürümden önceki bir ikiliye).** Eski ikilinin doğrulayıcısı `note`
  etkisini tanımaz (*"unknown effect"*). Bu sürümle materyalize olmuş bir tenant'ın — yeni
  tenant'lar ve 2026-10-18'den sonra ilk dokunuşunu yapanlar — saklı `cross-location note`
  belgesi eski ikilide **ayrıştırılamaz**: `assembleBaseline` onu eksik sayar, yeniden
  materyalize hiçbir şey yazmaz (`ON CONFLICT DO NOTHING`, türetilmiş id'li satır zaten var), ve `forTenant`
  **yalnız guardrail'lerle** karar verir — o tenant'ın her dokunuşu varsayılan `review`'a
  düşer, **kayıt yazılır** (§4.6, fail-safe) ve `policy.guardrails_alone`
  (`reason=incomplete_after_materialise`) operatörü çağırır (kural 8). *(Ölçüldü,
  2026-10-19: `433e172`'nin `forTenant`'ı, bu sürümün türetilmiş id'leriyle materyalize
  edilmiş dokuz belgeli bir tenant'ta — baseline 0, tenant 0, tek ERROR satırı o
  gerekçeyle; eski doğrulayıcı `unknown effect "note"` der.)* Aynı tenant'ın panelinde sahip `policy:edit`'ten de
  reddedilir (saklı küme hepsi-ya-hiç). Tenant katmanındaki bir `note` kopyası yalnız
  düşer (karar değişmez). 2026-07-26 baseline'ı saklı tenant'lar etkilenmez. **Yapılacak:**
  geri dönüş bir ileri düzeltmedir — bu sürüme (ya da üstüne) dönmek, onay kuyruğundaki
  kayıtları müdürün onaylaması; saklı belge değiştirilmez (`policy_versions` append-only,
  §4.3 gerekçesi). Etkilenen tenant'lar salt-okur sayılır: `policy_versions.document`'ında
  `"effect": "note"` olan ve `policies.layer = 'baseline'` olan satırlar. Ayrıntı:
  `deploy/README.md` *"M11 — deploy günü"* → *"Geri dönüş"*.

## Devam notu (2026-10-19) — kullanıcı kararları

1. **Panel koruması.** Eski `allow` (saklı 2026-07-26 baseline'ı ya da ondan
   2026-10-18'den önce alınmış bir tenant kopyası — mekâna bağlı ya da değil) etkinken bir
   **kanıt kuralı** (`policy.CrossLocationEvidenceSids`: IP proof, GPS-only allowance,
   no-evidence review — envanterin üç satırı; liste tek kaynaktır ve envanter testi onu
   ölçtüğüne eşit tutar) **kapatılamaz**: `tenant.RuleWriter.SetEnabled`
   `ErrCrossLocationFirst` döner, kural değişmez, aynı işlemde `policy.switch_refused`
   denetim satırı yazılır (`reason: old_cross_location_allow_in_force`, engelleyen
   kuralların adı/id'si), kurallar ekranı *"That change was not made"* başlığıyla önce
   *"Taptime baseline — cross-location note"*'u (ve ondan alınmış kopyaları) kapatmayı
   söyler. Aynı durum ters yönden de kapalı: bir kanıt kuralı kapalıyken eski `allow`'u
   yeniden açmak `ErrEvidenceRuleOff` (`reason: evidence_rule_off`). Kanıt kuralını açmak
   ve eski `allow`'u kapatmak hiç reddedilmez. Kapı sunucudadır (doğrudan POST da
   reddedilir); kilitler: hedef satır + karşı taraftaki her satır id sırasıyla, sonra
   yeniden okuma — iki zıt basış ikisi de geçemez (kilitlenme olursa biri `unavailable`,
   hiçbir şey yazılmaz). **Bugün zaten kapalı olanlar** (kanıt kuralı kapalı + eski
   `allow` açık — korumadan önce kurulmuş; prod'daki sayıyı ana oturum ölçer): saklı satır
   değişmez, delik eski `allow` kapatılana kadar durur; kapı o kapatmayı engellemez, kural
   ekranındaki duyuru ne değiştiğini söyler. Bir tenant'ın kendi **kanıt kuralı kopyası**
   kapsam dışıdır (kapatmak o mekânı baseline kuralına döndürür, kapı onu açık tutar).
2. **Çalışanın sonuç ekranı değişmez.** `tap.Decision.EmployeeNote` = kuralın cümlesi +
   yön notu, `note` etkilerinin cümleleri **hariç**; sonuç ekranı onu basar, kayıt notu
   (`Decision.Note`) aynen kalır. Ev şubesinde iki not aynı dizgidir (36 render'lık golden
   bayt-aynı).
3. **Duyuru** (`BaselineChanges`) üç kapalı durumu da söyler: GPS-only allowance kapalı
   (1/128), IP proof of place kapalı (4/128), no evidence needs review kapalı (1/128 — eski
   sessiz onay), ve eski kopya açıkken bu üçünün kapatılamayacağını. Metin sha256 ile
   pinli. *(2026-10-18 girdisi sevk edilmeden — `main`'e ulaşmadan — genişletildi; hiçbir
   tenant'a eski metin gösterilmedi.)*
4. **INFO satırı** süreç başına tenant başına bir kez (`oncePerTenant`, en çok 10 000
   tenant hatırlanır, en eskisi unutulur — unutmak yalnız satırı tekrarlar).
5. **Geri dönüş notu** yukarıda (*Sonuçlar*) ve `deploy/README.md`'de.
- ADR 0004 §2'nin *"beş effect"* tablosu altıya çıkar; `ignore`/`redirect` guardrail'e
  kapalı kalır (§6 değişmez).
