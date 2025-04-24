package network

/*
Congestion 定义了一个用于网络拥塞控制的接口。

该接口提供了拥塞控制算法所需的基本操作，包括初始化、接收确认、发送控制、状态更新和信息获取方法。

接入的算法应实现 Congestion 接口，以确保其能够进行有效的拥塞管理。

接口说明：

- Init()
  初始化拥塞控制状态，设置相关参数和数据结构，为后续操作做好准备。

- RecvAck(id int, size int)
  接收数据确认机制，更新已成功发送的数据量。该方法应在接收到确认时调用。

- CanSend(id int, size int) bool
  检查是否可以发送指定大小的数据。如果当前正在发送的数据量超过最大允许的飞行数据量，则返回 false；否则，更新当前正发送的数据量并返回 true。

- Update()
  更新拥塞控制的状态和参数，动态调整最大飞行数据量。根据当前的网络条件和发送数据的反馈信息调整控制策略。

- Info() string
  返回一个描述当前拥塞控制状态的信息字符串，用于调试和监测目的。

实现该接口的类型应具备相应的业务逻辑，以适应不同的网络条件和表现出合理的拥塞控制特性。
*/

type Congestion interface {
	Init()
	RecvAck(id int, size int)
	CanSend(id int, size int) bool
	Update()
	Info() string
}
