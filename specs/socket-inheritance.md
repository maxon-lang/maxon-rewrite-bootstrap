---
feature: socket-inheritance
status: experimental
keywords: [socket, tcp, listener, accept, subprocess, spawn, inherit, handle, close-on-exec, fin]
category: network
---

# A socket never outlives its close in a child the program spawned

## Documentation

Every socket the runtime opens is private to the process that opened it: the socket `TcpClient.connect`
creates, the listener `TcpListener.bind` creates, and the connection `TcpListener.accept` answers. A child
the program spawns afterwards receives none of them. So `close()` ends the connection at once, whatever
children are still running — the peer reading to the end of the connection sees that end when the socket
is closed, not when the last child that could have held a copy of it exits.

## Tests

<!-- test: socket-inheritance.an-accepted-socket-closes-while-a-spawned-child-lives -->
<!-- unsupported-targets: wasm32-wasi -->
The server accepts a connection, spawns a child that lives until the server closes the child's standard
input, then closes the accepted socket. The client, reading to the end of the connection, must see that end
while the child is still alive: the child leaves only after the client has been heard from, so a child
holding a copy of the accepted socket would keep the connection open for ever, and the case would not finish.
```maxon
function waitingChild() returns ExitCode
	var stdin = Console.stdin()
	let read = try stdin.readLine() otherwise return 2
	return match read 'read'
		ended gives 0
		line gives 1
	end 'read'
end 'waitingChild'

function readsItsEnd(port NetworkPort) returns bool
	var client = try TcpClient.connect("127.0.0.1", port: port) otherwise return false
	let heard = try client.readSome(64) otherwise 'clientEnded'
		try client.close() otherwise ignore
		return false
	end 'clientEnded'
	if heard.isEmpty() 'ended'
		try client.close() otherwise ignore
		return true
	end 'ended'
	print("unexpected bytes '{String.from(heard)}'\n")
	try client.close() otherwise ignore
	return false
end 'readsItsEnd'

function main() returns ExitCode
	if CommandLine.args().count() > 1 'iAmTheChild'
		return waitingChild()
	end 'iAmTheChild'
	let me = try Process.executablePath() otherwise return 2
	var listener = try TcpListener.bind("127.0.0.1", port: 0) otherwise return 3
	let reader = async readsItsEnd(listener.port())
	let conn = try listener.accept() otherwise 'refused4x27'
		reader.cancel()
		try listener.close() otherwise ignore
		return 4
	end 'refused4x27'
	var argv = StringArray.create(0)
	argv.push("child")
	var child = try StreamingSubprocess.spawn(MxExecutable.path(me), arguments: argv) otherwise 'refused5x31'
		reader.cancel()
		try listener.close() otherwise ignore
		try conn.close() otherwise ignore
		return 5
	end 'refused5x31'
	try conn.close() otherwise ignore
	let ended = await reader
	child.closeStdin()
	let code = try child.wait() otherwise 'listenerEnded'
		try listener.close() otherwise ignore
		return 6
	end 'listenerEnded'
	child.release()
	print("ended={ended} child={code}\n")
	try listener.close() otherwise ignore
	return 0
end 'main'
```
```stdout
ended=true child=0
```
```exitcode
0
```

<!-- test: socket-inheritance.a-connected-socket-closes-while-a-spawned-child-lives -->
<!-- unsupported-targets: wasm32-wasi -->
The mirror of the case above, for the socket `TcpClient.connect` creates rather than the one `accept`
answers. The server spawns a child that lives until the server closes the child's standard input and signals
the client, which closes its socket; the server, reading to the end of the connection, must see that end while
the child is still alive, because the server closes the child's input only after it has seen that end.
```maxon
function waitingChild() returns ExitCode
	var stdin = Console.stdin()
	let read = try stdin.readLine() otherwise return 2
	return match read 'read'
		ended gives 0
		line gives 1
	end 'read'
end 'waitingChild'

function closesOnSignal(port NetworkPort) returns bool
	var client = try TcpClient.connect("127.0.0.1", port: port) otherwise return false
	let signal = try client.readSome(1) otherwise 'clientEnded'
		try client.close() otherwise ignore
		return false
	end 'clientEnded'
	try client.close() otherwise ignore
	return String.from(signal) == "!"
end 'closesOnSignal'

function readsItsEnd(mutates conn TcpClient) returns bool
	let heard = try conn.readSome(64) otherwise return false
	if heard.isEmpty() 'ended'
		return true
	end 'ended'
	print("unexpected bytes '{String.from(heard)}'\n")
	return false
end 'readsItsEnd'

function main() returns ExitCode
	if CommandLine.args().count() > 1 'iAmTheChild'
		return waitingChild()
	end 'iAmTheChild'
	let me = try Process.executablePath() otherwise return 2
	var listener = try TcpListener.bind("127.0.0.1", port: 0) otherwise return 3
	let closer = async closesOnSignal(listener.port())
	var conn = try listener.accept() otherwise 'refused4x34'
		closer.cancel()
		try listener.close() otherwise ignore
		return 4
	end 'refused4x34'
	var argv = StringArray.create(0)
	argv.push("child")
	var child = try StreamingSubprocess.spawn(MxExecutable.path(me), arguments: argv) otherwise 'refused5x38'
		closer.cancel()
		try listener.close() otherwise ignore
		try conn.close() otherwise ignore
		return 5
	end 'refused5x38'
	_ = try conn.send("!") otherwise 'refused6x39'
		closer.cancel()
		try listener.close() otherwise ignore
		try conn.close() otherwise ignore
		return 6
	end 'refused6x39'
	let ended = readsItsEnd(conn)
	let signalled = await closer
	child.closeStdin()
	let code = try child.wait() otherwise 'listenerEnded'
		try listener.close() otherwise ignore
		try conn.close() otherwise ignore
		return 7
	end 'listenerEnded'
	child.release()
	print("signalled={signalled} ended={ended} child={code}\n")
	try listener.close() otherwise ignore
	try conn.close() otherwise ignore
	return 0
end 'main'
```
```stdout
signalled=true ended=true child=0
```
```exitcode
0
```
