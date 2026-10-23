--
-- PostgreSQL database dump
--

\restrict HLY4ML1888kexEpEBTiPTC4iMFeEHzr3e32wxy4Ye0eX4NXf5fE8h0LtXKkArUT

-- Dumped from database version 18.6
-- Dumped by pg_dump version 18.6

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: waterbody_display_pfas_sites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.waterbody_display_pfas_sites (
    group_id text NOT NULL,
    site_id bigint NOT NULL
);


--
-- Data for Name: waterbody_display_pfas_sites; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.waterbody_display_pfas_sites (group_id, site_id) FROM stdin;
hudson-river	2
hudson-river	35
hudson-river	36
hudson-river	47
hudson-river	48
\.


--
-- Name: waterbody_display_pfas_sites waterbody_display_pfas_sites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_pfas_sites
    ADD CONSTRAINT waterbody_display_pfas_sites_pkey PRIMARY KEY (group_id, site_id);


--
-- Name: waterbody_display_pfas_sites waterbody_display_pfas_sites_group_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_pfas_sites
    ADD CONSTRAINT waterbody_display_pfas_sites_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.waterbody_display_groups(id);


--
-- Name: waterbody_display_pfas_sites waterbody_display_pfas_sites_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_pfas_sites
    ADD CONSTRAINT waterbody_display_pfas_sites_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.pfas_sites(id);


--
-- PostgreSQL database dump complete
--

\unrestrict HLY4ML1888kexEpEBTiPTC4iMFeEHzr3e32wxy4Ye0eX4NXf5fE8h0LtXKkArUT

