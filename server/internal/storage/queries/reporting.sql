-- name: ValuationByLocation :many
SELECT
    i.location_id                                AS location_id,
    l.name                                        AS location_name,
    CAST(COUNT(*) AS INTEGER)                     AS item_count,
    CAST(COALESCE(SUM(COALESCE(p.purchase_price_minor, 0)), 0) AS INTEGER) AS total_value_minor
FROM items i
LEFT JOIN locations l
    ON l.group_id = sqlc.arg(group_id) AND l.id = i.location_id AND l.deleted_at IS NULL
LEFT JOIN item_purchase p
    ON p.group_id = sqlc.arg(group_id) AND p.item_id = i.id AND p.deleted_at IS NULL
WHERE i.group_id = sqlc.arg(group_id)
  AND i.deleted_at IS NULL
GROUP BY i.location_id
ORDER BY location_name ASC, location_id ASC;

-- name: ValuationByLabel :many
SELECT
    lb.id                                          AS label_id,
    lb.name                                        AS label_name,
    CAST(COUNT(*) AS INTEGER)                       AS item_count,
    CAST(COALESCE(SUM(COALESCE(p.purchase_price_minor, 0)), 0) AS INTEGER) AS total_value_minor
FROM item_labels il
JOIN items i
    ON i.group_id = sqlc.arg(group_id) AND i.id = il.item_id AND i.deleted_at IS NULL
JOIN labels lb
    ON lb.group_id = sqlc.arg(group_id) AND lb.id = il.label_id AND lb.deleted_at IS NULL
LEFT JOIN item_purchase p
    ON p.group_id = sqlc.arg(group_id) AND p.item_id = i.id AND p.deleted_at IS NULL
WHERE il.group_id = sqlc.arg(group_id)
  AND il.deleted_at IS NULL
GROUP BY lb.id, lb.name
ORDER BY lb.name ASC, lb.id ASC;

-- name: ListWarrantyExpiring :many
SELECT
    i.id           AS item_id,
    i.name         AS item_name,
    w.expires_on   AS expires_on
FROM items i
JOIN item_warranty w
    ON w.group_id = sqlc.arg(group_id) AND w.item_id = i.id AND w.deleted_at IS NULL
WHERE i.group_id = sqlc.arg(group_id)
  AND i.deleted_at IS NULL
  AND w.is_lifetime = 0
  AND w.expires_on IS NOT NULL
  AND w.expires_on >= sqlc.arg(from_date)
  AND w.expires_on <= sqlc.arg(to_date)
ORDER BY w.expires_on ASC, i.name ASC, i.id ASC;

-- name: ListPurchasesInRange :many
SELECT
    i.id                    AS item_id,
    i.name                  AS item_name,
    p.purchased_on          AS purchased_on,
    p.vendor                AS vendor,
    p.purchase_price_minor  AS purchase_price_minor
FROM items i
JOIN item_purchase p
    ON p.group_id = sqlc.arg(group_id) AND p.item_id = i.id AND p.deleted_at IS NULL
WHERE i.group_id = sqlc.arg(group_id)
  AND i.deleted_at IS NULL
  AND p.purchased_on IS NOT NULL
  AND p.purchased_on >= sqlc.arg(from_date)
  AND p.purchased_on <= sqlc.arg(to_date)
ORDER BY p.purchased_on ASC, i.name ASC, i.id ASC;

