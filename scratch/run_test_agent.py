import asyncio
import os
import sys
from google.antigravity import Agent, LocalAgentConfig

async def main():
    print("Initializing test agent with local connection...")
    
    # Use default configuration
    config = LocalAgentConfig()
    
    # Set PYTHONPATH to include our hooks folder so it gets dynamically loaded
    plugin_dir = os.path.expanduser("~/.gemini/config/plugins/hivemind_hooks")
    if plugin_dir not in sys.path:
        sys.path.append(plugin_dir)
        
    print(f"Dynamically loading hooks from: {plugin_dir}")
    try:
        import hivemind_hooks
        print("✔ Telemetry hooks imported successfully!")
    except ImportError as e:
        print(f"❌ Failed to import telemetry hooks: {e}")
        return

    print("Starting agent session...")
    async with Agent(config) as agent:
        print(f"Session started! Conversation ID: {agent.conversation_id}")
        
        print("Sending prompt to agent...")
        response = await agent.chat("Hi! Just say hello in one short sentence.")
        print(f"Agent response: {await response.text()}")
        
    print("Agent session finished!")

if __name__ == "__main__":
    asyncio.run(main())
