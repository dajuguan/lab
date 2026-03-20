# Simplified Version of TEE
- hardware is simulated with software
- isolation, memory encryption and integrety, chain of certificate is out of scope
- in scope:
    - image measurement
    - in memory private key to sign the quote
    - verify quote
```
cargo run -- register 
cargo run -- run --a 20 --b 22 --nonce demo-nonce
cargo run --  verify --nonce demo-nonce --quote state/demo-quote.json
```