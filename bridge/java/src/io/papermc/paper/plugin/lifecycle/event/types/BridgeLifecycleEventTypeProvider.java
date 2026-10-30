package io.papermc.paper.plugin.lifecycle.event.types;

import io.papermc.paper.plugin.lifecycle.event.LifecycleEvent;
import io.papermc.paper.plugin.lifecycle.event.LifecycleEventOwner;
import io.papermc.paper.plugin.lifecycle.event.handler.LifecycleEventHandler;
import io.papermc.paper.plugin.lifecycle.event.handler.configuration.LifecycleEventHandlerConfiguration;
import io.papermc.paper.plugin.lifecycle.event.handler.configuration.MonitorLifecycleEventHandlerConfiguration;
import io.papermc.paper.plugin.lifecycle.event.handler.configuration.PrioritizedLifecycleEventHandlerConfiguration;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Proxy;

/**
 * Lifecycle event types for the kremenmc bridge. It lives in Paper's package
 * because the provider interface is package-private. kremenmc.bridge.Brigadier
 * collects the configured handlers and fires the events it supports.
 */
public final class BridgeLifecycleEventTypeProvider implements LifecycleEventTypeProvider {
    /** A configured handler; the bridge's lifecycle manager receives these. */
    public interface Config {
        String eventName();

        LifecycleEventHandler<?> handler();

        int order();

        boolean isMonitor();
    }

    @Override
    public <O extends LifecycleEventOwner, E extends LifecycleEvent> LifecycleEventType.Monitorable<O, E> monitor(String name, Class<? extends O> owner) {
        return type(LifecycleEventType.Monitorable.class, name, MonitorLifecycleEventHandlerConfiguration.class);
    }

    @Override
    public <O extends LifecycleEventOwner, E extends LifecycleEvent> LifecycleEventType.Prioritizable<O, E> prioritized(String name, Class<? extends O> owner) {
        return type(LifecycleEventType.Prioritizable.class, name, PrioritizedLifecycleEventHandlerConfiguration.class);
    }

    @Override
    public TagEventTypeProvider tagProvider() {
        return proxy(TagEventTypeProvider.class, (p, m, a) -> switch (m.getName()) {
            case "preFlatten", "postFlatten" -> prioritized("tags/" + m.getName(), LifecycleEventOwner.class);
            default -> object(p, m.getName(), a, "TagEventTypeProvider");
        });
    }

    @SuppressWarnings("unchecked")
    private static <T> T type(Class<?> iface, String name, Class<?> configType) {
        return (T) proxy(iface, (p, m, a) -> switch (m.getName()) {
            case "name" -> name;
            case "newHandler" -> config(configType, name, (LifecycleEventHandler<?>) a[0]);
            default -> object(p, m.getName(), a, "LifecycleEventType{" + name + "}");
        });
    }

    private static LifecycleEventHandlerConfiguration<?> config(Class<?> configType, String name, LifecycleEventHandler<?> handler) {
        int[] order = {0};
        boolean[] monitor = {false};
        return (LifecycleEventHandlerConfiguration<?>) Proxy.newProxyInstance(BridgeLifecycleEventTypeProvider.class.getClassLoader(),
                new Class<?>[] {configType, Config.class}, (p, m, a) -> switch (m.getName()) {
                    case "eventName" -> name;
                    case "handler" -> handler;
                    case "order" -> order[0];
                    case "isMonitor" -> monitor[0];
                    case "priority" -> {
                        order[0] = (Integer) a[0];
                        yield p;
                    }
                    case "monitor" -> {
                        monitor[0] = true;
                        yield p;
                    }
                    default -> object(p, m.getName(), a, "LifecycleEventHandlerConfiguration{" + name + "}");
                });
    }

    private static Object object(Object p, String method, Object[] a, String str) {
        return switch (method) {
            case "equals" -> p == a[0];
            case "hashCode" -> System.identityHashCode(p);
            case "toString" -> str;
            default -> null;
        };
    }

    @SuppressWarnings("unchecked")
    private static <T> T proxy(Class<T> iface, InvocationHandler h) {
        return (T) Proxy.newProxyInstance(BridgeLifecycleEventTypeProvider.class.getClassLoader(), new Class<?>[] {iface}, h);
    }
}
