# TCP多路复用
- TCP本身stream是全双工的，但是TCP的本身并不提供多stream并发，必须按照字节流交付
- 所以TCP multiplexing一般指的是端口复用：不同应用可以同时用同一 IP，通过不同端口通信
## TCP应用层多路复用
- HTTP1.1基于TCP，但是因为没有frame/stream-id的概念，所以只能串行处理
- HTTP/2 frame + stream-id、Yamux frame + stream-id、QUIC stream则在同一个TCP连接/QUIC链接中同时处理多个链接请求
- 核心原理: 每一个stream都有唯一的ID，数据按stream交错传递，parser根据stream-id分发到对应的请求
    - 但是每一个stream内部还是按字节流传输的，因此单个stream内部的req/resp还是只能串行处理
