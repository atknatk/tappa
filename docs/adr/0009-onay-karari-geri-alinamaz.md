# ADR 0009 — FLAGGED onay kararı geri alınamaz, ve bunun ürün içinde bir telafi yolu yok

- **Durum:** kabul edildi (mevcut davranışın **kaydı**; kod değişmedi)
- **Tarih:** 2026-08-06
- **Karar veren:** kararın kendisi [M1-06 / Q20](../plan/open-questions.md#q20--onaylar-ayrı-transaction_reviews-tablosunda)
  ile 2026-07-24'te verildi ve migration `00005`'te uygulandı. **Bu ADR yeni bir karar
  almıyor** — M6-04'ün 6. turunda **iki denetçi bağımsız olarak** aynı şeyi bulunca
  yazıldı: sonucu hiçbir dosya söylemiyordu.
- **Etkilenen:** [`db/migrations/00005_create_transactions_audit_reviews.sql`](../../db/migrations/00005_create_transactions_audit_reviews.sql)
  · [`internal/domain/review`](../../internal/domain/review/review.go)
  · [`internal/handler/review.go`](../../internal/handler/review.go)
  · [M6-07 raporlar](../plan/m6-dashboard.md) (burada süresi doluyor)
- **Migration YOK.** Bu bir **belgeleme** kararıdır; şema 00005'ten beri aynı.

---

## Karar

Bir `flag` kaydına verilen onay/ret **bir kez** yazılır ve **hiçbir yoldan**
değiştirilemez, silinemez veya üzerine yazılamaz. Bu, üç ayrı mekanizmanın
kesişimidir ve üçü de kasıtlıdır:

| Mekanizma | Ne yapar | Nerede |
|---|---|---|
| `UNIQUE (transaction_id)` | ikinci bir karar satırı **var olamaz** | 00005, `transaction_reviews_txn_key` |
| `REVOKE UPDATE, DELETE` | `tappa_app` mevcut satırı değiştiremez | 00005 |
| `tappa_forbid_mutation` trigger | **`tappa_owner` bile** değiştiremez | 00005 |

**Ölçüldü (2026-08-06, `tappa_owner` = superuser olarak, geri alınan bir
transaction içinde):**

```
UPDATE refused: append-only table transaction_reviews: UPDATE is forbidden
                (section 4.3, use a new row + audit_log)
DELETE refused: append-only table transaction_reviews: DELETE is forbidden
                (section 4.3, use a new row + audit_log)
current_user = tappa_owner, usesuper = t
```

## Sonuç — ve neden yazılması gerekiyordu

**Yanlışlıkla Approve'a basan bir müdürün kararı kalıcıdır.** Ürünün içinde onu
düzeltecek hiçbir yol yoktur: ikinci bir karar `UNIQUE` yüzünden yazılamaz, mevcut
satır iki katmanda değiştirilemez. Tek çare bir **migration**'dır (ya da bir DBA
müdahalesi), ve o müdahalenin **ürün içinde `audit_log` izi olmaz** — çünkü izi
yazacak kod yoktur.

⚠️ **CLAUDE.md §4.3'ün kendi telafi cümlesi bu tabloda YAPISAL OLARAK
KULLANILAMAZ.** §4.3 *"düzeltme = yeni kayıt + `audit_log`"* diyor. `transactions`
için bu doğrudur ve işler. `transaction_reviews` için **"yeni kayıt" seçeneği
kapatılmıştır** — bunu kapatan şey de bilinçlidir ve gerekçesi 00005'te yazılıdır:

> *"bir kayit BIR KEZ karara baglanir. Aksi halde mudur approved/rejected/approved
> yazarak etkin sonucu istedigi kadar degistirir (transactions'a UPDATE yetkisi
> olmasa bile)."*

Yani iki §4.3-türevi kural birbirini kesiyor: *"geçmiş değişmez"* ve *"düzeltme yeni
bir satırdır"*. İkincisi burada birincinin lehine feda edilmiş. **Bu bir ihlal
değil** (güvenlik denetçisinin görüşü, 2026-08-06): korunması gereken şey **tap
kaydıdır** ve o kusursuz korunuyor; kararın tek-seferlik olması savunulabilir bir
tasarımdır. Kayda değer olan, **hiçbir dosyanın bunu söylememesiydi**.

## Bugünkü etki — ölçülmüş, sınırlı

- **Parasal etki yok.** `transaction_reviews`'ü okuyan **hiçbir rapor sorgusu yok**;
  tek okuyucular `db/queries/reviews.sql`'in üç `SELECT`'i ve
  `ListPanelTransactions`'ın `LEFT JOIN`'i (üreten komut:
  `grep -rn "transaction_reviews" db/queries/`).
- Etki bugün **sunumla sınırlı**: yanlış karar Transactions listesinde
  *"Approved/Rejected by a manager"* olarak görünür ve kuyruktan düşer.
- **Saat toplamı etkilenmiyor** çünkü saat toplamı henüz **yok** (M6-07).

## Ne zaman süresi doluyor

🔴 **M6-07 (raporlar ve CSV) geldiğinde etki parasal olur.** O görev `flag`
kayıtlarının saat toplamına girip girmeyeceğine karar verirken bu kararı okumak
zorundadır: yanlışlıkla `rejected` işaretlenmiş bir vardiya, o andan itibaren
**ödenmeyen bir vardiyadır** ve müdürün panelde düzeltme yolu yoktur.

M6-07 bu ADR'yi ya **kabul etmeli** (ve ekranda kararın kalıcı olduğunu **basma
öncesi** söylemeli) ya da **değiştirilmesini istemeli**.

## Düzeltmek isteyen biri hangi yolu izler

Bu ADR bir yol **önermiyor** — seçenekleri ve her birinin neyi feda ettiğini
yazıyor. Üçü de **migration**'dır.

1. **Kararı sürümlemek.** `UNIQUE (transaction_id)` kaldırılır, yerine
   `UNIQUE (transaction_id, superseded_by)` benzeri bir şekil veya *"en yeni
   `reviewed_at` kazanır"* okuması gelir. **Feda edilen:** 00005'in açıkça
   engellediği şey geri gelir — müdür etkin sonucu istediği kadar değiştirebilir.
   Bunu tolere etmenin şartı, her sürümün `audit_log`'a düşmesi ve raporun
   **hangi sürümü** kullandığını söylemesidir.
2. **Ayrı bir "itiraz" tablosu.** Karar dokunulmaz kalır; üstüne append-only bir
   `review_appeals` gelir ve **etkin durum JOIN ile** okunur. **Feda edilen:**
   üçüncü bir tablo ve "etkin durum" sorusunun her okuyucuda tekrar cevaplanması.
   §4.3'ün ruhuna en yakın olan bu.
3. **Hiçbir şey — ve ekranda söylemek.** Karar kalıcı kalır, ve onay formu bunu
   basmadan **önce** söyler. En ucuzu; **bugün bunu da yapmıyoruz** ve ekran metni
   kararın geri alınamaz olduğunu hiçbir yerde belirtmiyor.

⚠️ **Bu ADR ölçtüğünü yazar, fazlasını değil.** Yukarıdaki üç yolun hiçbiri
denenmedi, maliyetleri ölçülmedi; hangisinin doğru olduğu M6-07'nin (ya da bir
kullanıcı kararının) işidir. Burada kesin olan tek şey, ölçülen kısımdır: karar
kalıcıdır, üç mekanizma birden onu kalıcı kılar, ve ürünün içinde telafi yolu
yoktur.
