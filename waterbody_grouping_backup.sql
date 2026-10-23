--
-- PostgreSQL database dump
--

\restrict hh3zi70BQ0TVUnKpTN43cueNRvn7fbZ1QUASkDaqpXaJBZ5qezDIvaJWrgZTLUE

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
-- Name: waterbody_display_groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.waterbody_display_groups (
    id text NOT NULL,
    name text NOT NULL
);


--
-- Name: waterbody_display_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.waterbody_display_members (
    group_id text NOT NULL,
    waterbody_id text NOT NULL
);


--
-- Data for Name: waterbody_display_groups; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.waterbody_display_groups (id, name) FROM stdin;
hudson-river	Hudson River
\.


--
-- Data for Name: waterbody_display_members; Type: TABLE DATA; Schema: public; Owner: -
--

COPY public.waterbody_display_members (group_id, waterbody_id) FROM stdin;
hudson-river	ed2403c16b88b743cd528443
hudson-river	56175d42932223a93d0039bf
hudson-river	048fdb376e57b2bd1dea07ae
hudson-river	7f3d75bb8134ff1bbd04083e
hudson-river	31038e9141f7cc224d58e955
hudson-river	3d41679b57c748ab82357500
hudson-river	ef3d309da45e80e417d21733
hudson-river	0cdac377a070364afa9f23ac
hudson-river	231601c4231b45ed6a5ac367
hudson-river	5574774871dcf2149c4f678b
hudson-river	0a254d5fc44975305adc935b
hudson-river	a6df63839ce7603b886c023d
hudson-river	44baf6bb7065a3b20efebac9
hudson-river	ff69a76e1688d63c92390883
hudson-river	da2fd357c2f5334b2d60cd52
hudson-river	c052e498ed7e9f145bd701d2
hudson-river	4de03abf121cdb3681e9d0b7
hudson-river	e5af31d944230812aac597d8
\.


--
-- Name: waterbody_display_groups waterbody_display_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_groups
    ADD CONSTRAINT waterbody_display_groups_pkey PRIMARY KEY (id);


--
-- Name: waterbody_display_members waterbody_display_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_members
    ADD CONSTRAINT waterbody_display_members_pkey PRIMARY KEY (group_id, waterbody_id);


--
-- Name: waterbody_display_members waterbody_display_members_group_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_members
    ADD CONSTRAINT waterbody_display_members_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.waterbody_display_groups(id);


--
-- Name: waterbody_display_members waterbody_display_members_waterbody_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.waterbody_display_members
    ADD CONSTRAINT waterbody_display_members_waterbody_id_fkey FOREIGN KEY (waterbody_id) REFERENCES public.waterbodies_v2(id);


--
-- PostgreSQL database dump complete
--

\unrestrict hh3zi70BQ0TVUnKpTN43cueNRvn7fbZ1QUASkDaqpXaJBZ5qezDIvaJWrgZTLUE

