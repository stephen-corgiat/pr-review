# bookstore

The Acme Books storefront API: browse the catalog, fill a cart, check out.

```sh
go run .            # listens on :8080
go test ./...
```

## Endpoints

| Method | Path | Does |
|---|---|---|
| `GET` | `/books` | the catalog |
| `GET` | `/cart` | the cart and its total |
| `POST` | `/cart/items` | add a book: `{"isbn": "…", "qty": 1}` |
| `DELETE` | `/cart/items/{isbn}` | take a book out |

Prices are in cents. Shipping is $4.99, free once the subtotal reaches $35.
