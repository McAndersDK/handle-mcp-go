"""Start the real server over stdio, like an MCP client does, and list its tools.

Catches anything that writes to stdout (it would break JSON-RPC) and import errors.
No network and no logins: listing tools never calls a store.
"""

import os
import subprocess
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


def test_server_imports_without_system_time_zones():
    """Windows has no system time zone database (#91). PYTHONTZPATH="" hides ours,
    so zoneinfo must find Europe/Copenhagen in the tzdata package."""
    env = {**os.environ, "PYTHONTZPATH": ""}
    code = "import handle_mcp.server, handle_mcp.compare"
    result = subprocess.run(
        [sys.executable, "-c", code],
        check=False,
        timeout=60,
        env=env,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
