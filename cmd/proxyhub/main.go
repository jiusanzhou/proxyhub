// proxyhub - 聚合代理池服务
//
// 用法：
//
//	proxyhub serve --proxy-port 7000 --api-port 7001 --db ./proxyhub.db
//	proxyhub serve --config /etc/proxyhub.yaml
//	proxyhub version
//
// 启动后：
//
//	curl -x http://localhost:7000 https://example.com  # HTTP 前向代理
//	curl http://localhost:7001/api/v1/stats            # REST API
//	open http://localhost:7001/                        # Web Dashboard
package main

import (
	"log"

	"go.zoe.im/proxyhub/cmd"

	// register subcommands via init()
	_ "go.zoe.im/proxyhub/cmd/proxyhub/commands"
)

func main() {
	if err := cmd.Run(); err != nil {
		log.Fatalln(err)
	}
}
