## How to run
```
export CGO_CXXFLAGS="$(llvm-config --cxxflags)"
export CGO_LDFLAGS="$(llvm-config --ldflags --libs --system-libs)"
export CGO_CPPFLAGS="$(llvm-config --cppflags)"
export CGO_CFLAGS="$(llvm-config --cflags)"

cd compiler
go test -v -tags=llvm18
# go-llvm:
git co funcs

go test -tags=llvm18 -run ^TestArithmeticOpcodes$ github.com/QuarkChain/go-evmc/compiler -v
```

## Trouble shotting
### [Install llvm](https://apt.llvm.org/)
```
# tinygo.org/x/go-llvm
../../../go/pkg/mod/tinygo.org/x/go-llvm@v0.0.0-20221028183034-8341240c0b32/analysis.go:16:10: fatal error: 'llvm-c/Analysis.h' file not found
#include "llvm-c/Analysis.h" // If you are getting an error here you need to build or install LLVM, see https://tinygo.org/docs/guides/build/
```

### go-llvm branch
git co funcs 
```
# tinygo.org/x/go-llvm
../../go-llvm/executionengine.go:18:10: fatal error: executionengine.h: No such file or directory
   18 | #include "executionengine.h"
```


### change llvm version
```
# github.com/QuarkChain/go-evmc/compiler.test
/snap/go/10927/pkg/tool/linux_amd64/link: running g++ failed: exit status 1
/usr/bin/g++ -m64 -s -Wl,--build-id=0xeb40edbb09583e785d4c82be45fe0709bbf3d7ce -o $WORK/b001/compiler.test -Wl,--export-dynamic-symbol=_cgo_panic -Wl,--export-dynamic-symbol=_cgo_topofstack -Wl,--export-dynamic-symbol=crosscall2 -Wl,--compress-debug-sections=zlib /tmp/go-link-2203427294/go.o /tmp/go-link-2203427294/000000.o /tmp/go-link-2203427294/000001.o /tmp/go-link-2203427294/000002.o /tmp/go-link-2203427294/000003.o /tmp/go-link-2203427294/000004.o /tmp/go-link-2203427294/000005.o /tmp/go-link-2203427294/000006.o /tmp/go-link-2203427294/000007.o /tmp/go-link-2203427294/000008.o /tmp/go-link-2203427294/000009.o /tmp/go-link-2203427294/000010.o /tmp/go-link-2203427294/000011.o /tmp/go-link-2203427294/000012.o /tmp/go-link-2203427294/000013.o /tmp/go-link-2203427294/000014.o /tmp/go-link-2203427294/000015.o /tmp/go-link-2203427294/000016.o /tmp/go-link-2203427294/000017.o /tmp/go-link-2203427294/000018.o /tmp/go-link-2203427294/000019.o /tmp/go-link-2203427294/000020.o /tmp/go-link-2203427294/000021.o /tmp/go-link-2203427294/000022.o /tmp/go-link-2203427294/000023.o /tmp/go-link-2203427294/000024.o /tmp/go-link-2203427294/000025.o /tmp/go-link-2203427294/000026.o /tmp/go-link-2203427294/000027.o /tmp/go-link-2203427294/000028.o /tmp/go-link-2203427294/000029.o /tmp/go-link-2203427294/000030.o /tmp/go-link-2203427294/000031.o /tmp/go-link-2203427294/000032.o -O2 -g -O2 -g -L/usr/lib/llvm-20/lib -lLLVM-20 -O2 -g -lpthread -no-pie
/usr/bin/ld: cannot find -lLLVM-20: No such file or directory
collect2: error: ld returned 1 exit status
```

## Differences
- binop: build IR
- `call_mem_op`: => - `call_ir_builtin`: 
- `call_(fallible_)builtin`: 
- `contract_field` or `env_field`: => field => build llvm


## Backends
- [MLIR](https://x.com/class_lambda/status/1727390349574705277)
- [MLIR workshop](https://github.com/lambdaclass/mlir-workshop)
   - [rustic MLIR bindings](https://github.com/lambdaclass/melior)
