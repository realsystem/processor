#!/bin/bash
# Helper script to manage Processor services

set -e

command="${1:-help}"

case "$command" in
    start)
        echo "Starting Processor services..."
        echo ""
        echo "Terminal 1: Control Plane"
        echo "  cd control-plane && make dev"
        echo ""
        echo "Terminal 2: Data Plane Worker"
        echo "  cd data-plane && make dev"
        echo ""
        echo "Or use: ./scripts/manage.sh start-bg (to run in background)"
        ;;

    start-bg)
        echo "Starting services in background..."
        cd control-plane
        make dev > ../logs/control-plane.log 2>&1 &
        CONTROL_PID=$!
        echo "✅ Control plane started (PID: $CONTROL_PID)"

        sleep 2

        cd ../data-plane
        make dev > ../logs/worker.log 2>&1 &
        WORKER_PID=$!
        echo "✅ Worker started (PID: $WORKER_PID)"

        echo ""
        echo "View logs:"
        echo "  tail -f logs/control-plane.log"
        echo "  tail -f logs/worker.log"
        echo ""
        echo "Stop services:"
        echo "  ./scripts/manage.sh stop"
        ;;

    stop)
        echo "Stopping Processor services..."
        cd control-plane && make stop
        cd ../data-plane && make stop
        echo "✅ All services stopped"
        ;;

    status)
        echo "Processor Service Status:"
        echo ""

        if lsof -ti:8080 >/dev/null 2>&1; then
            PID=$(lsof -ti:8080)
            echo "✅ Control Plane: Running (PID: $PID, Port: 8080)"
        else
            echo "❌ Control Plane: Not running"
        fi

        if pgrep -f "./worker" >/dev/null 2>&1; then
            PID=$(pgrep -f "./worker")
            echo "✅ Worker: Running (PID: $PID)"
        else
            echo "❌ Worker: Not running"
        fi
        ;;

    logs)
        echo "Viewing logs (Ctrl+C to exit)..."
        if [ -f logs/control-plane.log ] && [ -f logs/worker.log ]; then
            tail -f logs/control-plane.log logs/worker.log
        else
            echo "❌ Log files not found. Services not started in background?"
            echo "   Run: ./scripts/manage.sh start-bg"
        fi
        ;;

    restart)
        echo "Restarting Processor services..."
        ./scripts/manage.sh stop
        sleep 1
        ./scripts/manage.sh start-bg
        ;;

    *)
        echo "Processor Service Manager"
        echo ""
        echo "Usage: ./scripts/manage.sh <command>"
        echo ""
        echo "Commands:"
        echo "  start      - Show commands to start services manually"
        echo "  start-bg   - Start services in background"
        echo "  stop       - Stop all services"
        echo "  restart    - Restart all services"
        echo "  status     - Show service status"
        echo "  logs       - Tail logs (for background services)"
        echo ""
        echo "Examples:"
        echo "  ./scripts/manage.sh start-bg    # Start in background"
        echo "  ./scripts/manage.sh status       # Check if running"
        echo "  ./scripts/manage.sh stop         # Stop everything"
        ;;
esac
