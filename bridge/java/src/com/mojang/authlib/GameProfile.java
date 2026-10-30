package com.mojang.authlib;

import com.mojang.authlib.properties.PropertyMap;
import java.util.UUID;

public record GameProfile(UUID id, String name, PropertyMap properties) {
    public GameProfile {
        if (properties == null) properties = new PropertyMap();
    }

    public GameProfile(UUID id, String name) { this(id, name, new PropertyMap()); }

    public UUID getId() { return id; }
    public String getName() { return name; }
    public PropertyMap getProperties() { return properties; }
}
