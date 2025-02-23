import random
import heapq
from dataclasses import dataclass
from typing import List, Tuple, TypeVar
import hashlib

K = 4
ID_BIT_SIZE = 12


def logDistance(n1, n2):
    dis = n1 ^ n2
    return dis.bit_length()


@dataclass
class NodeRecord:
    nid: int
    # ip: str
    # port: int


def keccak256(body):
    m = hashlib.sha3_256()
    m.update(bytes(body))
    # one hex str take 4 bits
    return int(m.hexdigest()[: ID_BIT_SIZE // 4], base=16)


Self = TypeVar("Self", bound="Node")


# data structure
class Node:
    def __init__(self, node: NodeRecord):
        self.routingTable = [list() for _ in range(ID_BIT_SIZE + 1)]
        self.node = node
        self.k = K

    def distance(self, otherId: int):
        return logDistance(self.node.nid, otherId)

    def addNode(self, n: Self):
        dist = self.distance(n.node.nid)
        kBucket = 1  # here self.k is not used to simulate larger hops
        if len(self.routingTable[dist]) < kBucket:
            tableIds = [n.node.nid for n in self.routingTable[dist]]
            if n.node.nid not in tableIds:
                self.routingTable[dist].append(n)
        else:
            # drop node according credit score
            pass

    def getRoutingTable(self):
        table = []
        for nodes in self.routingTable:
            table.append([])
            for node in nodes:
                table[-1].append(node.node)
        return table

    # find k closest
    def findNodeNeighbors(self, toId: int) -> List[Tuple[int, Self]]:
        dist = self.distance(toId)
        if dist == 0:
            return [(0, self)]

        pendingQ = []
        for i in range(1, ID_BIT_SIZE + 1):
            for item in self.routingTable[i]:
                dist = item.distance(toId)
                # print("dist======>", dist)
                if dist == 0:
                    return [(0, item)]
                if len(pendingQ) < self.k:
                    heapq.heappush(pendingQ, (-dist, item))
                elif dist < -pendingQ[0][0]:
                    heapq.heappushpop(pendingQ, (-dist, item))

        return pendingQ

    # for heapq comparing
    def __lt__(self, other: Self):
        return self.node.nid < other.node.nid

    def recursiveFindNode(self, toId: int) -> NodeRecord:
        print("===finding neighbors of starting node, with dist to target:", self.distance(toId))
        neighbors = self.findNodeNeighbors(toId)
        hp: List[Tuple[int, Self]] = []
        [heapq.heappush(hp, (-n[0], n[1])) for n in neighbors]
        visited = set()
        hop = 0
        while len(hp) > 0:
            print("hop=================================================>", hop)
            # if dist == 0, we found the node
            for dist, n in neighbors:
                print(
                    f"found neighbor:{n.node}, dist to target: {-dist}, routingTable: {n.getRoutingTable()}"
                )
                self.addNode(n)
                if dist == 0:
                    return n.node

            # pick α=1 closest nodes to the target it knows of; if α > 1, return should be done below
            item = heapq.heappop(hp)
            node = item[1]
            if node.node.nid in visited:
                continue

            visited.add(node.node.nid)
            #  sends (concurrent) FindNode packets to known nodes
            neighbors = node.findNodeNeighbors(toId)
            print(
                "===finding neighbors of node:",
                node.node.nid,
                ", with dist to target:",
                item[0],
            )
            # add learned nodes to heap
            for neighbor in neighbors:
                if neighbor[1].node.nid not in visited:
                    [heapq.heappush(hp, (-neighbor[0], neighbor[1]))]

            hop += 1


import unittest


class TestKad(unittest.TestCase):
    def setUp(self):
        super().setUp()
        nodes = [
            Node(NodeRecord(nid=keccak256(random.randint(0, 1 << ID_BIT_SIZE))))
            for _ in range(1000)
        ]

        for i in range(len(nodes)):
            for j in range(0, len(nodes)):
                if j == i:
                    continue
                nodes[i].addNode(nodes[j])
                nodes[j].addNode(nodes[i])

        self.nodes = nodes

    def testFindNodeNeighbors(self):
        me = self.nodes[0]
        to = self.nodes[random.randint(0,1000)]
        neighbors = me.findNodeNeighbors(to.node.nid)
        # print("neighbors start=======")
        # [print(fr"dist:{n[0]}, record:{n[1].node}") for n in neighbors]
        # print("neighbors end=========")

    def testFindNode(self):
        me = self.nodes[0]
        to = self.nodes[random.randint(0, len(self.nodes))]
        print("starting node:", me.node.nid, ", routingTable:", me.getRoutingTable())
        print("target node:", to.node.nid, ", routingTable:", to.getRoutingTable())
        target = me.recursiveFindNode(to.node.nid)
        print("=================================================")
        print(f"found target node for nid: {to.node.nid} is: {target}")


if __name__ == "__main__":
    seed = random.randint(0, 1 << 32 - 1)
    random.seed(1278769384)
    # random.seed(seed)
    # print("Current random seed:", seed)
    unittest.main()
