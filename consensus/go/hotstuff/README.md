# Basic HotStuff
## Q: 如何保证GST之后不同节点的延迟不一致，新leader也能收到2f+1个newView，同时其他节点不超时?
假设部分节点在T时刻超时，立刻发送newView
节点A在`δ1`收到了PrepareMSG,因此会在`T+δ1`发送newView
```
--T(newView)|----------------------------------------------------------------------|timeout:4*δ1
            |
            |---|T+δ1:sendNewView
                    |T+2δ1:leadeReceiveNewView
                        |T+3δ1:ReceivePrepareFromLeader=> resetTimeout
```
可以看出其他节点和leader均不会timeout

## Q: 旧leader A超时，此时follower B发送newView给新leader C，但此次C还认为自己是follower如何处理?
- 如果不处理，会导致miss B的msg，导致view切换不成功
- 如果直接切换view也不行，因为有可能只是A和B之间通信断了，或者B是Byzantine节点。需要确保恶意follower不能随便切换view，因此需要满足如下条件:
    - B.view >= C.view: 先收集节点对B.view的投票，一旦有2f+1节点投票，则进行ViewChange

## Q: leader何时resetTimer?
- 每次进入下一个阶段才重置，而不是每次收到follower的消息才重置timer，否则leader可能永远不会意识到自己超时了

## Q: Commit阶段，follower已经设置过prepareQC，但是相应leader超时，那么prepareQC是否应该被commit？
- 该prepareQC对应的Block应该随着new Proposal被一起commit