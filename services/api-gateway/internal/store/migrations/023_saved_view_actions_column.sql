-- Views saved before the Actions column existed included every then-visible column.
-- Preserve that intent without changing views saved after the column became configurable.
UPDATE incident.saved_views
SET filters=jsonb_set(filters,'{columns}',filters->'columns' || '["actions"]'::jsonb)
WHERE filters ? 'columns' AND jsonb_typeof(filters->'columns')='array'
  AND NOT (filters->'columns' ? 'actions');
