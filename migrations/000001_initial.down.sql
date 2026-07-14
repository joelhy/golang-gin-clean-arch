-- Drop dependents before their referenced parents so rollback never relies on
-- disabling foreign-key checks or weakening referential integrity.
DROP TABLE idempotency_keys;
DROP TABLE stock_adjustments;
DROP TABLE order_items;
DROP TABLE orders;
DROP TABLE products;
DROP TABLE refresh_tokens;
DROP TABLE sessions;
DROP TABLE role_permissions;
DROP TABLE user_roles;
DROP TABLE permissions;
DROP TABLE roles;
DROP TABLE users;
