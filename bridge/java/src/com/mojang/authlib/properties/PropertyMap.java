package com.mojang.authlib.properties;

import com.google.common.collect.ForwardingMultimap;
import com.google.common.collect.LinkedHashMultimap;
import com.google.common.collect.Multimap;

public final class PropertyMap extends ForwardingMultimap<String, Property> {
    private final Multimap<String, Property> properties;

    public PropertyMap() { properties = LinkedHashMultimap.create(); }

    public PropertyMap(Multimap<String, Property> properties) { this.properties = LinkedHashMultimap.create(properties); }

    @Override
    protected Multimap<String, Property> delegate() { return properties; }
}
