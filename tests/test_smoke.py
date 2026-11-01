"""Start the real server over stdio, like an MCP client does, and list its tools.

Catches anything that writes to stdout (it would break JSON-RPC) and import errors.
No network and no logins: listing tools never calls a store.
"""

import sys

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

from tests.test_tools import ALLOWED


async def test_server_starts_over_stdio_and_lists_tools(tmp_path):
    params = StdioServerParameters(
        command=sys.executable,
        args=["-m", "handle_mcp.server"],
        env={"HANDLE_CONFIG_DIR": str(tmp_path)},  # no real logins
    )
    async with stdio_client(params) as (read, write), ClientSession(read, write) as session:
        await session.initialize()
        tools = await session.list_tools()
    assert sorted(t.name for t in tools.tools) == sorted(ALLOWED)
