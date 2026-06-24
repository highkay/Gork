## Summary

<!-- 改动了什么，为什么改 -->

## Testing

<!-- 怎么验证：本地跑了什么、curl 结果、或"无需验证（仅文档/格式）" -->

- [ ] Targeted Go package test, if applicable
- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `staticcheck -checks="all,-ST*,-S1011,-U1000,-SA1019,-SA4006,-S1017,-S1016,-SA4011" ./...`
- [ ] `git diff --check`

## Risk Areas

- [ ] Auth or admin access control
- [ ] Config loading, validation, or env override
- [ ] Storage, cache, migrations, or persistence
- [ ] Proxy selection, clearance, or upstream networking
- [ ] Upstream protocol compatibility
- [ ] Static frontend or WebUI/Admin route behavior

## Related

<!-- Closes #... -->
