import os
import sys
import grpc
from concurrent import futures
import plugin_pb2
import plugin_pb2_grpc

class MyPlugin(plugin_pb2_grpc.WidgetPluginServicer):
    def FetchData(self, request, context):
        # Return data to the host
        return plugin_pb2.FetchResponse(data="Plugin Data Here")
    
    def Render(self, request, context):
        # Return rendered string (supports ANSI colors)
        return plugin_pb2.RenderResponse(rendered_string="\033[32mPlugin Active\033[0m")

def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    plugin_pb2_grpc.add_WidgetPluginServicer_to_server(MyPlugin(), server)
    
    # Use Windows Named Pipe passed by the Dashboard host via environment variable
    pipe_path = os.environ.get("PLUGIN_PIPE")
    if pipe_path:
        # Format for gRPC C-Core: pipe:\\.\pipe\... or pipe:pipe_name
        server_address = f"pipe:{pipe_path}" if not pipe_path.startswith("pipe:") else pipe_path
    else:
        # Fallback local pipe for testing
        server_address = r"pipe:\\.\pipe\dashboard_plugin_sample"
    
    server.add_insecure_port(server_address)
    server.start()
    server.wait_for_termination()

if __name__ == '__main__':
    serve()